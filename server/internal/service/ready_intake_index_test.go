package service

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"qimeng.local/next/server/internal/store"
)

func TestReadyIntakePollSeeksAuthorizedLibraryAndLimitsOldestFirst(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "synthetic.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	_, err = s.DB.Exec(`INSERT INTO libraries(id,name,kind,data) VALUES('allowed','Allowed','regular','{"enabled":true}');
	 WITH RECURSIVE nums(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM nums WHERE n<22)
	 INSERT INTO batches(id,created_at,data)
	 SELECT printf('batch-%02d',n),printf('2026-01-%02d',n),json_object('libraryId','allowed','mode','intelligent','state','receiving',
	 'manifestState','sealed','manifestFingerprint',printf('%064x',n),'expectedInputs',1,'maxResearchCalls',CASE WHEN n=1 THEN 3 ELSE 0 END) FROM nums;
	 INSERT INTO uploads(id,batch_id,state,data) SELECT 'up-'||id,id,'staged',json_object('hash',printf('%064x',1)) FROM batches;
	 INSERT INTO batch_inputs(batch_id,input_id,ordinal,upload_id,data) SELECT id,'synthetic-input',0,'up-'||id,'{}' FROM batches;`)
	if err != nil {
		t.Fatal(err)
	}
	const libraries = `["allowed"]`
	plans, err := s.DB.Query("EXPLAIN QUERY PLAN "+readyIntakeList, libraries, 0)
	if err != nil {
		t.Fatal(err)
	}
	var details []string
	for plans.Next() {
		var id, parent, unused int
		var detail string
		if err = plans.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err = plans.Err(); err != nil {
		t.Fatal(err)
	}
	plans.Close()
	if plan := strings.Join(details, "\n"); !strings.Contains(plan, "SEARCH b USING INDEX batches_intelligent_intake") {
		t.Fatalf("discovery lost its per-library active-intake seek:\n%s", plan)
	}
	rows, err := s.DB.Query(readyIntakeList, libraries, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var b ReadyIntakeBatch
		if err = rows.Scan(&b.BatchID, &b.LibraryID, &b.ManifestFingerprint, &b.MaxResearchCalls); err != nil {
			t.Fatal(err)
		}
		if b.BatchID != fmt.Sprintf("batch-%02d", count+2) || b.MaxResearchCalls != 0 {
			t.Fatal("queue did not respect submission order or the grant budget")
		}
		count++
	}
	if err = rows.Err(); err != nil || count != 16 {
		t.Fatal("expected exactly 16 offers", err, count)
	}
}
