package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"strconv"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func profileRevision(r *http.Request) (int64, error) {
	value, err := strconv.ParseInt(r.URL.Query().Get("expectedRevision"), 10, 64)
	if err != nil || value < 0 {
		return 0, service.Fail(400, "revision_required", "必须提供当前资料块修订号。")
	}
	return value, nil
}

func (a *API) GetAuthorUpdates(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.AuthorUpdates(r.Context(), id) })
}

func (a *API) SetAuthorUpdates(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.SetAuthorUpdatesParams) {
	write[service.AuthorUpdatesInput](a, w, r, true, func(r *http.Request, w *service.Write, in service.AuthorUpdatesInput) (any, int, error) {
		v, e := a.s.SetAuthorUpdates(r.Context(), w, id, in)
		return v, 200, e
	})
}

func (a *API) CheckAuthorUpdates(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.CheckAuthorUpdatesParams) {
	if !allowScope(w, r, "read") {
		return
	}
	in, raw, e := decode[service.AuthorUpdateCheck](w, r)
	if e != nil {
		fail(w, e)
		return
	}
	v, e := a.s.CheckAuthorUpdates(r.Context(), principal(r), id, r.Header.Get("Idempotency-Key"), raw, in)
	if e != nil {
		fail(w, e)
		return
	}
	result(w, v)
}

func (a *API) GetAuthorProfile(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.AuthorProfile(r.Context(), principal(r), id)
	})
}

func (a *API) GetAuthorProfileSummary(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.AuthorProfileSummary(r.Context(), principal(r), id)
	})
}

func (a *API) UpdateAuthorProfileBlock(w http.ResponseWriter, r *http.Request, id generated.Id, block generated.ProfileBlockName, params generated.UpdateAuthorProfileBlockParams) {
	write[service.ProfileWrite](a, w, r, true, func(r *http.Request, w *service.Write, in service.ProfileWrite) (any, int, error) {
		value, err := a.s.UpdateAuthorProfile(r.Context(), w, id, string(block), in)
		return value, 200, err
	})
}

func (a *API) ListAuthorProfileCandidates(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ListAuthorProfileCandidatesParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, err := limit(r)
		if err != nil {
			return nil, err
		}
		return a.s.AuthorProfileCandidates(r.Context(), id, r.URL.Query().Get("archiveId"), r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) SubmitAuthorProfileCandidate(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.SubmitAuthorProfileCandidateParams) {
	write[service.ProfileCandidateCreate](a, w, r, true, func(r *http.Request, w *service.Write, in service.ProfileCandidateCreate) (any, int, error) {
		value, err := a.s.SubmitAuthorProfileCandidate(r.Context(), w, id, in)
		return value, 201, err
	})
}

func (a *API) DismissAuthorProfileCandidate(w http.ResponseWriter, r *http.Request, id generated.Id, candidateId string, params generated.DismissAuthorProfileCandidateParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		value, err := a.s.DismissAuthorProfileCandidate(r.Context(), w, id, candidateId)
		return value, 200, err
	})
}

func (a *API) FetchAuthorProfileAvatar(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.FetchAuthorProfileAvatarParams) {
	if !allowScope(w, r, "read") {
		return
	}
	in, raw, err := decode[service.ProfileAvatarFetch](w, r)
	if err != nil {
		fail(w, err)
		return
	}
	raw = append(raw, []byte("\x00"+r.URL.Query().Encode())...)
	value, err := a.s.FetchAuthorProfileAvatar(r.Context(), principal(r), id, r.Header.Get("Idempotency-Key"), raw, in)
	if err != nil {
		fail(w, err)
		return
	}
	result(w, value)
}

func (a *API) UploadAuthorProfileAvatar(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.UploadAuthorProfileAvatarParams) {
	if !allowScope(w, r, "read") {
		return
	}
	if err := service.Authorize(principal(r), "admin", ""); err != nil {
		fail(w, err)
		return
	}
	revision, err := profileRevision(r)
	if err != nil {
		fail(w, err)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		fail(w, service.Fail(413, "avatar_too_large", "头像原图最多 4 MiB。"))
		return
	}
	value, err := a.s.UploadAuthorProfileAvatar(r.Context(), principal(r), id, r.Header.Get("Idempotency-Key"), revision, body)
	if err != nil {
		fail(w, err)
		return
	}
	result(w, value)
}

func (a *API) ClearAuthorProfileAvatar(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ClearAuthorProfileAvatarParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		revision, err := profileRevision(r)
		if err != nil {
			return nil, 0, err
		}
		value, err := a.s.ClearAuthorProfileAvatar(r.Context(), w, id, revision)
		return value, 200, err
	})
}

func (a *API) GetAuthorProfileAvatar(w http.ResponseWriter, r *http.Request, id generated.Id) {
	if !allowScope(w, r, "read") {
		return
	}
	body, modified, err := a.s.AuthorProfileAvatar(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", `"`+id+`"`)
	http.ServeContent(w, r, "avatar.png", modified, bytes.NewReader(body))
}

func (a *API) ListAuthorProfileArchives(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ListAuthorProfileArchivesParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, err := limit(r)
		if err != nil {
			return nil, err
		}
		return a.s.AuthorProfileArchives(r.Context(), id, r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) GetAuthorProfileArchive(w http.ResponseWriter, r *http.Request, id generated.Id, archiveId string) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.AuthorProfileArchive(r.Context(), id, archiveId)
	})
}
