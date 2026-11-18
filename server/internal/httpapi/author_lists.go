package httpapi

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"unicode/utf8"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) ListAuthorLists(w http.ResponseWriter, r *http.Request, params generated.ListAuthorListsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		if e := service.Authorize(principal(r), "admin", ""); e != nil {
			return nil, e
		}
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.AuthorLists(r.Context(), r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) GetAuthorList(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		if e := service.Authorize(principal(r), "admin", ""); e != nil {
			return nil, e
		}
		return a.s.AuthorList(r.Context(), id)
	})
}

func (a *API) ImportAuthorList(w http.ResponseWriter, r *http.Request, params generated.ImportAuthorListParams) {
	// A supplementary Unicode character may occupy twelve escaped bytes in JSON.
	writeScoped[service.AuthorListInput](a, w, r, "admin", true, func(r *http.Request, w *service.Write, in service.AuthorListInput) (any, int, error) {
		if utf8.RuneCountInString(in.Content) > 1<<20 {
			return nil, 0, service.Fail(413, "author_list_too_large", "作者清单超过协议字符上限。")
		}
		v, e := a.s.ImportAuthorList(r.Context(), w, in)
		return v, 200, e
	}, (12<<20)+(4<<10))
}

func (a *API) RebuildAuthorListMatches(w http.ResponseWriter, r *http.Request, params generated.RebuildAuthorListMatchesParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		v, e := a.s.RebuildAuthorLists(r.Context(), w)
		return v, 200, e
	})
}

func (a *API) DeleteAuthorList(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.DeleteAuthorListParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		revision, e := revision(r)
		if e != nil {
			return nil, 0, e
		}
		return nil, 204, a.s.DeleteAuthorList(r.Context(), w, id, revision)
	})
}

func (a *API) ExportMasterAuthorList(w http.ResponseWriter, r *http.Request) {
	a.writeAuthorDocument(w, r, "")
}

func (a *API) ExportAuthorList(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.writeAuthorDocument(w, r, id)
}

func (a *API) GetAuthorListMirror(w http.ResponseWriter, r *http.Request) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.AuthorListMirror(r.Context(), principal(r)) })
}

func (a *API) UpdateAuthorListMirror(w http.ResponseWriter, r *http.Request, params generated.UpdateAuthorListMirrorParams) {
	write[service.AuthorListMirrorUpdate](a, w, r, true, func(r *http.Request, w *service.Write, in service.AuthorListMirrorUpdate) (any, int, error) {
		v, e := a.s.UpdateAuthorListMirror(r.Context(), w, in)
		return v, 200, e
	})
}

func (a *API) writeAuthorDocument(w http.ResponseWriter, r *http.Request, id string) {
	if !allowScope(w, r, "read") {
		return
	}
	body, err := a.s.ExportAuthorList(r.Context(), principal(r), id)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", fmt.Sprintf("\"%x\"", sha256.Sum256([]byte(body))))
	fmt.Fprint(w, body)
}
