package httpapi

import (
	"bytes"
	"net/http"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) GetBackupChangeState(w http.ResponseWriter, r *http.Request) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.BackupChangeState(r.Context(), principal(r)) })
}

func (a *API) ExportBackupAuthorDocuments(w http.ResponseWriter, r *http.Request, id generated.Id) {
	if !allowScope(w, r, "read") {
		return
	}
	data, backup, err := a.s.BackupAuthorDocuments(r.Context(), principal(r), id)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", "attachment; filename=authors.md; filename*=UTF-8''%E4%BD%9C%E8%80%85%E8%B5%84%E6%96%99.md")
	http.ServeContent(w, r, "authors.md", backup.CreatedAt, bytes.NewReader(data))
}

func (a *API) ListBackups(w http.ResponseWriter, r *http.Request, params generated.ListBackupsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.Backups(r.Context(), principal(r), r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) ListBackupTargets(w http.ResponseWriter, r *http.Request) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.BackupTargets(r.Context(), principal(r)) })
}

func (a *API) ListSourceCleanupPermits(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ListSourceCleanupPermitsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.CleanupPermits(r.Context(), principal(r), id, r.URL.Query().Get("inputId"), r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) GetSourceCleanupPermit(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.CleanupPermit(r.Context(), principal(r), id)
	})
}

func (a *API) IssueSourceCleanupPermit(w http.ResponseWriter, r *http.Request, id generated.Id, protectionId string, params generated.IssueSourceCleanupPermitParams) {
	if !allowScope(w, r, "read") {
		return
	}
	in, raw, e := decode[service.CleanupPermitInput](w, r)
	if e != nil {
		fail(w, e)
		return
	}
	out, e := a.s.IssueCleanupPermit(r.Context(), principal(r), id, protectionId, r.Header.Get("Idempotency-Key"), raw, in)
	if e != nil {
		fail(w, e)
		return
	}
	result(w, out)
}

func (a *API) RecordSourceCleanupReceipt(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.RecordSourceCleanupReceiptParams) {
	write[service.CleanupReceiptInput](a, w, r, true, func(r *http.Request, w *service.Write, in service.CleanupReceiptInput) (any, int, error) {
		p, e := a.s.RecordCleanupReceipt(r.Context(), w, id, in)
		return p, 200, e
	})
}

func (a *API) ListBatchProtections(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ListBatchProtectionsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.BatchProtections(r.Context(), principal(r), id, r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) GetBatchProtection(w http.ResponseWriter, r *http.Request, id generated.Id, protectionId string) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.BatchProtection(r.Context(), principal(r), id, protectionId)
	})
}

func (a *API) ProtectBatchInputs(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ProtectBatchInputsParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		p, e := a.s.StartBatchProtection(r.Context(), w, id)
		return p, 202, e
	})
}

func (a *API) RetryBatchProtection(w http.ResponseWriter, r *http.Request, id generated.Id, protectionId string, params generated.RetryBatchProtectionParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		p, e := a.s.RetryBatchProtection(r.Context(), w, id, protectionId)
		return p, 202, e
	})
}

func (a *API) GetBackup(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.Backup(r.Context(), principal(r), id) })
}

func (a *API) EstimateBackupExport(w http.ResponseWriter, r *http.Request) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.EstimateBackupExport(r.Context(), principal(r))
	})
}

func (a *API) DiscardBackup(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.DiscardBackupParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		if e := a.s.DiscardBackup(r.Context(), w, id); e != nil {
			return nil, 0, e
		}
		return nil, 204, nil
	})
}

func (a *API) ExportBackup(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ExportBackupParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		b, e := a.s.StartBackupExport(r.Context(), w, id)
		return b, 202, e
	})
}

func (a *API) DownloadBackupArchive(w http.ResponseWriter, r *http.Request, id generated.Id) {
	if !allowScope(w, r, "read") {
		return
	}
	file, b, e := a.s.BackupArchive(r.Context(), principal(r), id)
	if e != nil {
		fail(w, e)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="qimeng-`+b.ID+`.qnext"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("ETag", `"`+b.ArchiveHash+`"`)
	http.ServeContent(w, r, "backup.qnext", *b.ExportedAt, file)
}
