package httpapi

import (
	"net/http"

	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

func (a *API) RenameCollection(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.RenameCollectionParams) {
	write[generated.CollectionRename](a, w, r, true, func(r *http.Request, w *service.Write, in generated.CollectionRename) (any, int, error) {
		v, err := a.s.RenameCollection(r.Context(), w, id, in.Name, in.ExpectedRevision)
		return v, 200, err
	})
}

func (a *API) SetCollectionAliases(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.SetCollectionAliasesParams) {
	writeScoped[generated.AliasSetUpdate](a, w, r, "admin", true, func(r *http.Request, tx *service.Write, in generated.AliasSetUpdate) (any, int, error) {
		c, err := a.s.SetCollectionAliases(r.Context(), tx, id, in.ExpectedRevision, in.Aliases)
		return c, http.StatusOK, err
	})
}

func (a *API) ListCollectionMembers(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.ListCollectionMembersParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, err := limit(r)
		if err != nil {
			return nil, err
		}
		return a.s.CollectionMembers(r.Context(), id, r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) SetCollectionMemberPosition(w http.ResponseWriter, r *http.Request, id generated.Id, assetId string, params generated.SetCollectionMemberPositionParams) {
	write[generated.CollectionPositionUpdate](a, w, r, true, func(r *http.Request, w *service.Write, in generated.CollectionPositionUpdate) (any, int, error) {
		v, err := a.s.SetCollectionMemberPosition(r.Context(), w, id, assetId, in.Position, in.ExpectedRevision)
		return v, 200, err
	})
}
