package store

import (
	"path/filepath"
	"testing"
)

func TestUpgradeReleasesCancelledReservationsWithoutHidingPreparedOrPublishedInputs(t *testing.T) {
	database := filepath.Join(t.TempDir(), "abandoned.sqlite")
	s, err := Open(database)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.DB.Close() }()
	_, err = s.DB.Exec(`INSERT INTO batches VALUES('closed','','{"id":"closed","state":"cancelled"}'),('active','','{"id":"active","state":"receiving"}');
	 INSERT INTO uploads VALUES
	  ('staged','closed','staged','{"state":"staged","offset":7}'),
	  ('inflight','closed','staged','{"state":"staged","offset":7}'),
	  ('finished','closed','ingested','{"state":"ingested","assetId":"keep","publishedContentVersion":4,"offset":7}'),
	  ('other','active','receiving','{"state":"receiving","offset":7}');
	 INSERT INTO batch_inputs(batch_id,input_id,ordinal,upload_id,data) VALUES
	  ('closed','first',0,'staged','{}'),('closed','pending',1,'inflight','{}'),('closed','done',2,'finished','{}'),
	  ('closed','missing',3,NULL,'{}'),('active','open',0,'other','{}');
	 INSERT INTO file_intents(id,kind,state,data) VALUES('writing','ingest','prepared','{"UploadID":"inflight"}');
	 DELETE FROM schema_migrations WHERE version=29;`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(database)
	if err != nil {
		t.Fatal(err)
	}
	s = reopened
	var facts string
	err = s.DB.QueryRow(`SELECT group_concat(input_id||'='||cancelled||'/'||coalesce(state,'absent')||'/'||coalesce(json_extract(data,'$.offset'),0),',')
	 FROM (SELECT i.input_id,i.cancelled,u.state,u.data FROM batch_inputs i LEFT JOIN uploads u ON i.upload_id=u.id ORDER BY i.input_id)`).Scan(&facts)
	if err != nil {
		t.Fatal(err)
	}
	if facts != "done=0/ingested/7,first=1/cancelled/7,missing=1/absent/0,open=0/receiving/7,pending=0/staged/7" {
		t.Fatal("unexpected upgraded input facts", facts)
	}
}
