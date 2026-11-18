package httpapi

import (
	"net/http"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) GetLibrary(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.Library(r.Context(), id) })
}

func (a *API) GetAcquisitionVocabulary(w http.ResponseWriter, r *http.Request) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.AcquisitionVocabulary(r.Context()) })
}

func (a *API) UpdateAcquisitionVocabulary(w http.ResponseWriter, r *http.Request, params generated.UpdateAcquisitionVocabularyParams) {
	write[service.AcquisitionVocabularyUpdate](a, w, r, true, func(r *http.Request, w *service.Write, in service.AcquisitionVocabularyUpdate) (any, int, error) {
		v, e := a.s.UpdateAcquisitionVocabulary(r.Context(), w, in)
		return v, 200, e
	})
}

func (a *API) DeleteAuthor(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.DeleteAuthorParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		rev, e := revision(r)
		if e != nil {
			return nil, 0, e
		}
		return nil, 204, a.s.DeleteAuthor(r.Context(), w, id, rev)
	})
}

func (a *API) MergeAuthors(w http.ResponseWriter, r *http.Request, params generated.MergeAuthorsParams) {
	write[service.AuthorMergeInput](a, w, r, true, func(r *http.Request, w *service.Write, in service.AuthorMergeInput) (any, int, error) {
		v, e := a.s.MergeAuthors(r.Context(), w, in)
		return v, 200, e
	})
}

func (a *API) UpdateLibrary(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.UpdateLibraryParams) {
	write[service.LibraryUpdate](a, w, r, true, func(r *http.Request, w *service.Write, in service.LibraryUpdate) (any, int, error) {
		v, e := a.s.UpdateLibrary(r.Context(), w, id, in)
		return v, 200, e
	})
}

func (a *API) DeleteLibrary(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.DeleteLibraryParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		rev, e := revision(r)
		if e != nil {
			return nil, 0, e
		}
		return nil, 204, a.s.DeleteLibrary(r.Context(), w, id, rev)
	})
}

func (a *API) ListDirectories(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ListDirectoriesParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.Directories(r.Context(), id, r.URL.Query().Get("parent"), r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) CreateDirectory(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.CreateDirectoryParams) {
	write[struct {
		RelativePath string `json:"relativePath"`
	}](a, w, r, true, func(r *http.Request, w *service.Write, in struct {
		RelativePath string `json:"relativePath"`
	}) (any, int, error) {
		v, e := a.s.CreateDirectory(r.Context(), w, id, in.RelativePath)
		return v, 201, e
	})
}

func (a *API) ListTagVocabulary(w http.ResponseWriter, r *http.Request, params generated.ListTagVocabularyParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.TagVocabulary(r.Context(), r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) CreateTag(w http.ResponseWriter, r *http.Request, params generated.CreateTagParams) {
	write[struct {
		Name string `json:"name"`
	}](a, w, r, true, func(r *http.Request, w *service.Write, in struct {
		Name string `json:"name"`
	}) (any, int, error) {
		v, e := a.s.CreateTag(r.Context(), w, in.Name)
		return v, 201, e
	})
}

func (a *API) RenameTag(w http.ResponseWriter, r *http.Request, tag string, params generated.RenameTagParams) {
	write[struct {
		Name             string `json:"name"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}](a, w, r, true, func(r *http.Request, w *service.Write, in struct {
		Name             string `json:"name"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}) (any, int, error) {
		v, e := a.s.ChangeTag(r.Context(), w, tag, in.Name, in.ExpectedRevision, false)
		return v, 200, e
	})
}

func (a *API) DeleteTag(w http.ResponseWriter, r *http.Request, tag string, params generated.DeleteTagParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		rev, e := revision(r)
		if e != nil {
			return nil, 0, e
		}
		_, e = a.s.ChangeTag(r.Context(), w, tag, "", rev, true)
		return nil, 204, e
	})
}

func (a *API) RemoveAssetTag(w http.ResponseWriter, r *http.Request, id generated.Id, tag string, params generated.RemoveAssetTagParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		rev, e := revision(r)
		if e != nil {
			return nil, 0, e
		}
		v, e := a.s.RemoveAssetTag(r.Context(), w, id, tag, rev)
		return v, 200, e
	})
}

func (a *API) ListTimelineMarks(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ListTimelineMarksParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.Timeline(r.Context(), id, r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) CreateTimelineMark(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.CreateTimelineMarkParams) {
	write[service.TimelineInput](a, w, r, true, func(r *http.Request, w *service.Write, in service.TimelineInput) (any, int, error) {
		v, e := a.s.SaveMark(r.Context(), w, id, "", in)
		return v, 201, e
	})
}

func (a *API) UpdateTimelineMark(w http.ResponseWriter, r *http.Request, id generated.Id, markId string, params generated.UpdateTimelineMarkParams) {
	write[service.TimelineInput](a, w, r, true, func(r *http.Request, w *service.Write, in service.TimelineInput) (any, int, error) {
		v, e := a.s.SaveMark(r.Context(), w, id, markId, in)
		return v, 200, e
	})
}

func (a *API) DeleteTimelineMark(w http.ResponseWriter, r *http.Request, id generated.Id, markId string, params generated.DeleteTimelineMarkParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		rev, e := revision(r)
		if e != nil {
			return nil, 0, e
		}
		return nil, 204, a.s.DeleteMark(r.Context(), w, id, markId, rev)
	})
}

func (a *API) ListSources(w http.ResponseWriter, r *http.Request, params generated.ListSourcesParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.Sources(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) CreateSource(w http.ResponseWriter, r *http.Request, params generated.CreateSourceParams) {
	write[service.TermInput](a, w, r, true, func(r *http.Request, w *service.Write, in service.TermInput) (any, int, error) {
		v, e := a.s.SaveSource(r.Context(), w, "", in)
		return v, 201, e
	})
}

func (a *API) UpdateSource(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.UpdateSourceParams) {
	write[service.TermInput](a, w, r, true, func(r *http.Request, w *service.Write, in service.TermInput) (any, int, error) {
		v, e := a.s.SaveSource(r.Context(), w, id, in)
		return v, 200, e
	})
}

func (a *API) ListCharacters(w http.ResponseWriter, r *http.Request, params generated.ListCharactersParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		q := r.URL.Query()
		return a.s.Characters(r.Context(), q.Get("sourceId"), q.Get("q"), q.Get("cursor"), n)
	})
}

func (a *API) CreateCharacter(w http.ResponseWriter, r *http.Request, params generated.CreateCharacterParams) {
	write[service.TermInput](a, w, r, true, func(r *http.Request, w *service.Write, in service.TermInput) (any, int, error) {
		v, e := a.s.SaveCharacter(r.Context(), w, "", in)
		return v, 201, e
	})
}

func (a *API) UpdateCharacter(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.UpdateCharacterParams) {
	write[service.TermInput](a, w, r, true, func(r *http.Request, w *service.Write, in service.TermInput) (any, int, error) {
		v, e := a.s.SaveCharacter(r.Context(), w, id, in)
		return v, 200, e
	})
}
