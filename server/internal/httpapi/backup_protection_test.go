package httpapi_test

import (
	"testing"

	"qimeng.local/next/server/internal/service"
)

func TestFailureDomainLabelCannotMakeSameVolumeIndependent(t *testing.T) {
	f := setup(t)
	f.s.Config.BackupTargets = map[string]service.BackupTarget{"synthetic-target": {Path: t.TempDir(), FailureDomain: "operator-label-does-not-change-a-volume"}}
	f.s.Config.DefaultBackupTarget = "synthetic-target"
	u, _ := f.upload("same-volume-source.png", 199, nil)
	out := f.complete(u)
	r := f.json("POST", "/batches/"+out.Upload.BatchID+"/protections", "same-volume-protection", struct{}{}, 409)
	if r.Body.String() == "" {
		t.Fatal("independence rejection omitted reason")
	}
	var count int
	if e := f.s.DB.DB.QueryRow("SELECT COUNT(*) FROM batch_protections").Scan(&count); e != nil || count != 0 {
		t.Fatal("same-volume copy produced a protection proof")
	}
}
