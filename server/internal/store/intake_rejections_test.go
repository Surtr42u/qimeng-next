package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestUpgradeRecordsOnlyUnreceivedUnsupportedInputs(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "original.sqlite"), CatalogDatabase)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(migrationLedgerSQL); err != nil {
		t.Fatal(err)
	}
	steps, err := readSchemaMigrations(migrations)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.version >= 41 {
			break
		}
		if err = step.apply(db); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO batches VALUES('active','','{"state":"receiving","manifestState":"sealed","revision":8,"manifestFingerprint":"original"}'),
 ('closed','','{"state":"cancelled","manifestState":"sealed","revision":5,"manifestFingerprint":"cancelled"}');
 INSERT INTO uploads VALUES('published','active','ingested','{"assetId":"accepted","publishedContentVersion":4}');
 INSERT INTO batch_inputs(batch_id,input_id,ordinal,cancelled,upload_id,data) VALUES
 ('active','empty',0,0,NULL,'{"originalFileName":"image.png","size":0}'),
 ('active','text',1,0,NULL,'{"originalFileName":"notes.TxT","size":12}'),
 ('active','valid',2,0,NULL,'{"originalFileName":"picture.JPEG","size":12}'),
 ('active','unknown',3,0,NULL,'{"nameState":"unavailable"}'),
 ('active','noext',4,0,NULL,'{"originalFileName":"without-extension"}'),
 ('active','done',5,0,'published','{"originalFileName":"corrected.txt","size":12}'),
 ('active','skipped',6,1,NULL,'{"originalFileName":"old.txt"}'),
 ('closed','historical',0,0,NULL,'{"originalFileName":"notes.txt"}');
 INSERT INTO idempotency VALUES('owner','old-operation','old-key','old-request','original-receipt',200);`); err != nil {
		t.Fatal(err)
	}
	if err = applySchema(db, migrations); err != nil {
		t.Fatal(err)
	}
	var rejected, fingerprint, batchState, closedState, receipt, published string
	var revision int64
	err = db.QueryRow(`SELECT (SELECT group_concat(input_id||'='||rejection_code,',') FROM
 (SELECT input_id,rejection_code FROM batch_inputs WHERE rejection_code!='' ORDER BY ordinal)),
 json_extract(data,'$.manifestFingerprint'),json_extract(data,'$.state'),json_extract(data,'$.revision'),
 (SELECT json_extract(data,'$.state') FROM batches WHERE id='closed'),
 (SELECT response FROM idempotency WHERE key='old-key'),
 (SELECT u.data FROM uploads u JOIN batch_inputs i ON u.id=i.upload_id WHERE i.input_id='done')
 FROM batches WHERE id='active'`).Scan(&rejected, &fingerprint, &batchState, &revision, &closedState, &receipt, &published)
	if err != nil || rejected != "empty=empty_input,text=unsupported_media" || fingerprint != "original" ||
		batchState != "partial" || revision != 8 || closedState != "cancelled" || receipt != "original-receipt" ||
		published != `{"assetId":"accepted","publishedContentVersion":4}` {
		t.Fatal("upgraded selection or prior receipt changed unexpectedly", rejected, fingerprint, batchState, revision, closedState, receipt, published, err)
	}
	if err = ValidateBackupSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}
