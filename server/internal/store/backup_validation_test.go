package store

import (
 "context"
 "path/filepath"
 "testing"
)

func TestBackupSchemaRejectsExtraTriggerWithAuthenticMigrationLedger(t *testing.T){
 db,e:=Open(filepath.Join(t.TempDir(),"synthetic.sqlite"));if e!=nil{t.Fatal(e)};defer db.DB.Close();if e=ValidateBackupSchema(context.Background(),db.DB);e!=nil{t.Fatal("fresh schema did not validate",e)}
 if _,e=db.DB.Exec("CREATE TRIGGER hidden_restore_write AFTER DELETE ON credentials BEGIN UPDATE settings SET value='changed' WHERE key='instance_id'; END");e!=nil{t.Fatal(e)}
 if e=ValidateBackupSchema(context.Background(),db.DB);e==nil{t.Fatal("a valid migration ledger authorized an extra trigger")}
}
