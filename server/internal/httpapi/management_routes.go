package httpapi

import (
	"net/http"

	"qimeng.local/next/server/internal/service"
)

func (a *API) managementRoutes() {
	a.get("/libraries/{id}", func(r *http.Request) (any, error) { return a.s.Library(r.Context(), r.PathValue("id")) })
	a.get("/acquisition-vocabulary", func(r *http.Request) (any, error) { return a.s.AcquisitionVocabulary(r.Context()) })
	write[service.AcquisitionVocabularyUpdate](a, "PUT", "/acquisition-vocabulary", true, func(r *http.Request, w *service.Write, in service.AcquisitionVocabularyUpdate) (any, int, error) {
		v, e := a.s.UpdateAcquisitionVocabulary(r.Context(), w, in)
		return v, 200, e
	})
	write[struct{}](a, "DELETE", "/authors/{id}", true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		rev, e := revision(r)
		if e != nil {
			return nil, 0, e
		}
		return nil, 204, a.s.DeleteAuthor(r.Context(), w, r.PathValue("id"), rev)
	})
	write[service.AuthorMergeInput](a, "POST", "/authors/merge", true, func(r *http.Request, w *service.Write, in service.AuthorMergeInput) (any, int, error) {
		v, e := a.s.MergeAuthors(r.Context(), w, in)
		return v, 200, e
	})
	write[service.LibraryUpdate](a, "PATCH", "/libraries/{id}", true, func(r *http.Request, w *service.Write, in service.LibraryUpdate) (any, int, error) {
		v, e := a.s.UpdateLibrary(r.Context(), w, r.PathValue("id"), in)
		return v, 200, e
	})
	write[struct{}](a, "DELETE", "/libraries/{id}", true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		rev, e := revision(r)
		if e != nil {
			return nil, 0, e
		}
		return nil, 204, a.s.DeleteLibrary(r.Context(), w, r.PathValue("id"), rev)
	})
	a.get("/libraries/{id}/directories", func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.Directories(r.Context(), r.PathValue("id"), r.URL.Query().Get("parent"), r.URL.Query().Get("cursor"), n)
	})
	write[struct {
		RelativePath string `json:"relativePath"`
	}](a, "POST", "/libraries/{id}/directories", true, func(r *http.Request, w *service.Write, in struct {
		RelativePath string `json:"relativePath"`
	}) (any, int, error) { v, e := a.s.CreateDirectory(r.Context(), w, r.PathValue("id"), in.RelativePath); return v, 201, e })
	a.get("/tag-vocabulary", func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.TagVocabulary(r.Context(), r.URL.Query().Get("cursor"), n)
	})
	write[struct {
		Name string `json:"name"`
	}](a, "POST", "/tags", true, func(r *http.Request, w *service.Write, in struct {
		Name string `json:"name"`
	}) (any, int, error) { v, e := a.s.CreateTag(r.Context(), w, in.Name); return v, 201, e })
	write[struct {
		Name             string `json:"name"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}](a, "PATCH", "/tags/{tag}", true, func(r *http.Request, w *service.Write, in struct {
		Name             string `json:"name"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}) (any, int, error) { v, e := a.s.ChangeTag(r.Context(), w, r.PathValue("tag"), in.Name, in.ExpectedRevision, false); return v, 200, e })
	write[struct{}](a, "DELETE", "/tags/{tag}", true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		rev, e := revision(r)
		if e != nil {
			return nil, 0, e
		}
		_, e = a.s.ChangeTag(r.Context(), w, r.PathValue("tag"), "", rev, true)
		return nil, 204, e
	})
	write[struct{}](a, "DELETE", "/assets/{id}/tags/{tag}", true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		rev, e := revision(r)
		if e != nil {
			return nil, 0, e
		}
		v, e := a.s.RemoveAssetTag(r.Context(), w, r.PathValue("id"), r.PathValue("tag"), rev)
		return v, 200, e
	})
	a.get("/assets/{id}/timeline", func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.Timeline(r.Context(), r.PathValue("id"), r.URL.Query().Get("cursor"), n)
	})
	write[service.TimelineInput](a, "POST", "/assets/{id}/timeline", true, func(r *http.Request, w *service.Write, in service.TimelineInput) (any, int, error) {
		v, e := a.s.SaveMark(r.Context(), w, r.PathValue("id"), "", in)
		return v, 201, e
	})
	write[service.TimelineInput](a, "PUT", "/assets/{id}/timeline/{markId}", true, func(r *http.Request, w *service.Write, in service.TimelineInput) (any, int, error) {
		v, e := a.s.SaveMark(r.Context(), w, r.PathValue("id"), r.PathValue("markId"), in)
		return v, 200, e
	})
	write[struct{}](a, "DELETE", "/assets/{id}/timeline/{markId}", true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		rev, e := revision(r)
		if e != nil {
			return nil, 0, e
		}
		return nil, 204, a.s.DeleteMark(r.Context(), w, r.PathValue("id"), r.PathValue("markId"), rev)
	})
	a.get("/sources", func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.Sources(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("cursor"), n)
	})
	write[service.TermInput](a, "POST", "/sources", true, func(r *http.Request, w *service.Write, in service.TermInput) (any, int, error) {
		v, e := a.s.SaveSource(r.Context(), w, "", in)
		return v, 201, e
	})
	write[service.TermInput](a, "PUT", "/sources/{id}", true, func(r *http.Request, w *service.Write, in service.TermInput) (any, int, error) {
		v, e := a.s.SaveSource(r.Context(), w, r.PathValue("id"), in)
		return v, 200, e
	})
	a.get("/characters", func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		q := r.URL.Query()
		return a.s.Characters(r.Context(), q.Get("sourceId"), q.Get("q"), q.Get("cursor"), n)
	})
	write[service.TermInput](a, "POST", "/characters", true, func(r *http.Request, w *service.Write, in service.TermInput) (any, int, error) {
		v, e := a.s.SaveCharacter(r.Context(), w, "", in)
		return v, 201, e
	})
	write[service.TermInput](a, "PUT", "/characters/{id}", true, func(r *http.Request, w *service.Write, in service.TermInput) (any, int, error) {
		v, e := a.s.SaveCharacter(r.Context(), w, r.PathValue("id"), in)
		return v, 200, e
	})
}
