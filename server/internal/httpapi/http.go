// Package httpapi translates the versioned protocol into application use cases.
package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/service"
	"qimeng.local/next/server/internal/transport/generated"
)

type principalKey struct{}
type API struct {
	s *service.Service
}

var _ generated.ServerInterface = (*API)(nil)

func New(s *service.Service) http.Handler {
	a := &API{s: s}
	return a.routes(a)
}

func (a *API) routes(operations generated.ServerInterface) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.healthz)
	mux.HandleFunc("GET /metrics", a.auth(a.GetServiceMetrics))
	contract := embeddedContract()
	router := &authenticatedRouter{ServeMux: mux, api: a, contract: contract}
	handler := generated.HandlerWithOptions(operations, generated.StdHTTPServerOptions{
		BaseURL: contract.baseURL, BaseRouter: router, ErrorHandlerFunc: parameterError,
	})
	if router.registered != len(contract.operations) {
		panic("generated routes are missing OpenAPI operations")
	}
	return handler
}
func principal(r *http.Request) model.Principal {
	return r.Context().Value(principalKey{}).(model.Principal)
}
func fail(w http.ResponseWriter, err error) {
	var app *service.Error
	if errors.As(service.Normalize(err), &app) {
		respond(w, app.Status, app)
		return
	}
	// Never log credentials, source names, private paths or driver error strings.
	slog.Error("request failed", "errorType", errorType(err))
	respond(w, 500, &service.Error{Code: "internal_error", Message: "服务器操作失败，原内容保留。请使用同一操作身份回读状态。"})
}
func errorType(err error) string {
	if err == nil {
		return "none"
	}
	return "io_or_persistence"
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if status != 204 {
		if e := json.NewEncoder(w).Encode(value); e != nil {
			slog.Error("response write failed")
		}
	}
}
func result(w http.ResponseWriter, r service.Result) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(r.Status)
	if len(r.Body) > 0 {
		if _, e := w.Write(r.Body); e != nil {
			slog.Error("response write failed")
		}
	}
}
func originOK(r *http.Request, secureOrigin ...bool) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, e := url.Parse(o)
	if e != nil {
		return false
	}
	scheme := "http"
	if r.TLS != nil || len(secureOrigin) > 0 && secureOrigin[0] {
		scheme = "https"
	}
	return u.Scheme == scheme && u.Host == r.Host && u.User == nil
}
func (a *API) auth(next http.HandlerFunc) http.HandlerFunc { return a.authScope("read", next) }
func (a *API) authScope(scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Value(principalKey{}).(model.Principal); ok {
			if allowScope(w, r, scope) {
				next(w, r)
			}
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == r.Header.Get("Authorization") && token != "" {
			fail(w, service.Fail(401, "invalid_authorization", "仅支持 Bearer 凭据。"))
			return
		}
		cookieAuth := false
		if token == "" {
			if c, e := r.Cookie("qimeng_session"); e == nil {
				token = c.Value
				cookieAuth = true
			}
		}
		if cookieAuth && r.Method != "GET" && r.Method != "HEAD" && !originOK(r, a.s.Config.SecureCookies) {
			fail(w, service.Fail(403, "origin_denied", "请求来源与当前服务不一致。"))
			return
		}
		p, e := a.s.Authenticate(r.Context(), token)
		if e != nil {
			fail(w, e)
			return
		}
		if scope != "" && !p.Has(scope) {
			fail(w, service.Fail(403, "scope_denied", "凭据没有此操作权限。"))
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	}
}
func (a *API) readJSON(w http.ResponseWriter, r *http.Request, fn func(*http.Request) (any, error)) {
	a.readScoped(w, r, "read", fn)
}

func (a *API) readScoped(w http.ResponseWriter, r *http.Request, scope string, fn func(*http.Request) (any, error)) {
	if !allowScope(w, r, scope) {
		return
	}
	v, err := fn(r)
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, 200, v)
}

func decode[T any](w http.ResponseWriter, r *http.Request, bodyLimit ...int64) (T, []byte, error) {
	var v T
	maxBytes := int64(1 << 20)
	if len(bodyLimit) > 0 {
		maxBytes = bodyLimit[0]
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	raw, e := io.ReadAll(r.Body)
	if e != nil {
		return v, nil, service.Fail(413, "body_too_large", "请求正文超出限制。")
	}
	if e = validateJSONRequest(r, raw); e != nil {
		return v, nil, e
	}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if e = d.Decode(&v); e != nil {
		return v, nil, service.Fail(400, "invalid_json", "请求字段或 JSON 格式无效。")
	}
	if d.Decode(new(any)) != io.EOF {
		return v, nil, service.Fail(400, "invalid_json", "请求包含多余 JSON 数据。")
	}
	canonical, e := json.Marshal(v)
	return v, canonical, e
}
func write[T any](a *API, w http.ResponseWriter, r *http.Request, idempotent bool, fn func(*http.Request, *service.Write, T) (any, int, error)) {
	writeScoped(a, w, r, "read", idempotent, fn)
}
func writeScoped[T any](a *API, w http.ResponseWriter, r *http.Request, scope string, idempotent bool, fn func(*http.Request, *service.Write, T) (any, int, error), bodyLimit ...int64) {
	if !allowScope(w, r, scope) {
		return
	}
	in, raw, e := decode[T](w, r, bodyLimit...)
	if e != nil {
		fail(w, e)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if idempotent && key == "" {
		fail(w, service.Fail(400, "idempotency_required", "此操作必须提供稳定幂等键。"))
		return
	}
	// Include query preconditions in the receipt identity as well as the body.
	raw = append(raw, []byte("\x00"+r.URL.Query().Encode())...)
	out, e := a.s.Run(r.Context(), principal(r), r.Method+" "+r.URL.Path, key, raw, func(tx *service.Write) (any, int, error) { return fn(r, tx, in) })
	if e != nil {
		fail(w, e)
		return
	}
	result(w, out)
}
func limit(r *http.Request) (int, error) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return 0, nil
	}
	n, e := strconv.Atoi(v)
	if e != nil {
		return 0, service.Fail(400, "invalid_limit", "分页数量无效。")
	}
	return n, nil
}
func revision(r *http.Request) (int64, error) {
	v, e := strconv.ParseInt(r.URL.Query().Get("expectedRevision"), 10, 64)
	if e != nil || v < 1 {
		return 0, service.Fail(400, "revision_required", "必须提供当前修订号。")
	}
	return v, nil
}
func contentVersion(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("version")
	if raw == "" {
		return 0, nil
	}
	v, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || v < 1 {
		return 0, service.Fail(400, "invalid_version", "内容版本必须为正整数。")
	}
	return v, nil
}
func boolQuery(r *http.Request, name string) (*bool, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return nil, nil
	}
	b, e := strconv.ParseBool(v)
	if e != nil {
		return nil, service.Fail(400, "invalid_filter", "筛选参数无效。")
	}
	return &b, nil
}

type revisionBody struct {
	ExpectedRevision int64 `json:"expectedRevision"`
}
type batchCreateRequest struct {
	LibraryID        string `json:"libraryId"`
	Mode             string `json:"mode"`
	Label            string `json:"label"`
	MaxResearchCalls *int   `json:"maxResearchCalls"`
}
type collectionCreateRequest struct {
	LibraryID string   `json:"libraryId"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Aliases   []string `json:"aliases"`
}
type libraryCreateRequest struct {
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	StorageID        string `json:"storageId"`
	RootRelativePath string `json:"rootRelativePath"`
}

func (a *API) healthz(w http.ResponseWriter, r *http.Request) {
	if e := a.s.CheckReadiness(r.Context()); e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, map[string]string{"status": "ok"})
}

func (a *API) GetInstance(w http.ResponseWriter, r *http.Request) {
	respond(w, 200, map[string]any{"id": a.s.InstanceID, "name": "绮梦 Next", "apiVersion": "0.1.0", "capabilities": map[string]bool{"aiConfigured": a.s.Config.AIConfigured, "recommendations": true, "rankings": true, "stats": true, "backups": true, "migration": true}})
}

func (a *API) CreateSession(w http.ResponseWriter, r *http.Request) {
	if !originOK(r, a.s.Config.SecureCookies) {
		fail(w, service.Fail(403, "origin_denied", "请求来源不匹配。"))
		return
	}
	in, _, e := decode[service.SessionInput](w, r)
	if e != nil {
		fail(w, e)
		return
	}
	p, e := a.s.AuthenticateLogin(r.Context(), in)
	if e != nil {
		fail(w, e)
		return
	}
	out, e := a.s.Run(r.Context(), p, "createSession", "", nil, func(tx *service.Write) (any, int, error) {
		v, e := a.s.CreateSession(r.Context(), tx, in.Label, "", nil)
		return v, 201, e
	})
	if e != nil {
		fail(w, e)
		return
	}
	var session model.Principal
	if e = json.Unmarshal(out.Body, &session); e != nil {
		fail(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "qimeng_session", Value: session.Token, Path: "/", HttpOnly: true, Secure: a.s.Config.SecureCookies, SameSite: http.SameSiteStrictMode, Expires: session.ExpiresAt})
	result(w, out)
}

func (a *API) GetCurrentSession(w http.ResponseWriter, r *http.Request) {
	respond(w, 200, principal(r))
}

func (a *API) DeleteCurrentSession(w http.ResponseWriter, r *http.Request) {
	if !allowScope(w, r, "read") {
		return
	}
	p := principal(r)
	out, e := a.s.Run(r.Context(), p, "logout", "", nil, func(tx *service.Write) (any, int, error) { return nil, 204, a.s.Revoke(r.Context(), tx, p.ID) })
	if e != nil {
		fail(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "qimeng_session", Value: "", Path: "/", HttpOnly: true, Secure: a.s.Config.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	result(w, out)
}

func (a *API) CreateCredential(w http.ResponseWriter, r *http.Request) {
	issueSecret[credentialRequest](a, w, r, func(r *http.Request, w *service.Write, in credentialRequest) (any, int, error) {
		if (in.BatchID == "") == (in.AuthorTaskID == "") {
			return nil, 0, service.Fail(400, "task_required", "AI 凭据必须且只能绑定一个接收或资料维护任务。")
		}
		v, e := a.s.CreateSession(r.Context(), w, in.Label, in.BatchID, in.Scopes, in.AuthorTaskID)
		return v, 201, e
	})
}

func (a *API) RevokeCredential(w http.ResponseWriter, r *http.Request, id generated.Id) {
	write[struct{}](a, w, r, false, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		return nil, 204, a.s.Revoke(r.Context(), w, id)
	})
}

func (a *API) ListLibraries(w http.ResponseWriter, r *http.Request, params generated.ListLibrariesParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.Libraries(r.Context(), r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) ListStorages(w http.ResponseWriter, r *http.Request) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.Storages(), nil })
}

func (a *API) CreateLibrary(w http.ResponseWriter, r *http.Request, params generated.CreateLibraryParams) {
	write[libraryCreateRequest](a, w, r, true, func(r *http.Request, w *service.Write, in libraryCreateRequest) (any, int, error) {
		v, e := a.s.CreateLibrary(r.Context(), w, in.Name, in.Kind, in.StorageID, in.RootRelativePath)
		return v, 201, e
	})
}

func (a *API) ListAssets(w http.ResponseWriter, r *http.Request, params generated.ListAssetsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		f, e := assetFilterRequest(r)
		if e != nil {
			return nil, e
		}
		f.DateCountsBy = r.URL.Query().Get("dateCountsBy")
		return a.s.Assets(r.Context(), f)
	})
}

func (a *API) GetAsset(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.AssetDetail(r.Context(), id) })
}

func (a *API) UpdateAsset(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.UpdateAssetParams) {
	write[service.AssetUpdate](a, w, r, false, func(r *http.Request, w *service.Write, in service.AssetUpdate) (any, int, error) {
		v, e := a.s.UpdateAsset(r.Context(), w, id, in)
		return v, 200, e
	})
}

func (a *API) UpdateAssetTags(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.UpdateAssetTagsParams) {
	write[struct {
		ExpectedRevision int64    `json:"expectedRevision"`
		Tags             []string `json:"tags"`
	}](a, w, r, true, func(r *http.Request, w *service.Write, in struct {
		ExpectedRevision int64    `json:"expectedRevision"`
		Tags             []string `json:"tags"`
	}) (any, int, error) {
		v, e := a.s.UpdateTags(r.Context(), w, id, in.ExpectedRevision, in.Tags)
		return v, 200, e
	})
}

func (a *API) SetAssetFavorite(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.SetAssetFavoriteParams) {
	write[generated.AssetFavoriteUpdate](a, w, r, true, a.setAssetFavorite)
}

func (a *API) SetAssetTodayLike(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.SetAssetTodayLikeParams) {
	write[generated.AssetTodayLikeUpdate](a, w, r, true, a.setAssetTodayLike)
}

func (a *API) UpdateReaction(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.UpdateReactionParams) {
	write[generated.ReactionUpdate](a, w, r, true, a.updateReaction)
}

func (a *API) RecordEvent(w http.ResponseWriter, r *http.Request, id generated.Id) {
	write[generated.EventCreate](a, w, r, false, func(r *http.Request, w *service.Write, in generated.EventCreate) (any, int, error) {
		event := model.Event{EventID: in.EventId, Kind: string(in.Kind), ContentVersion: in.ContentVersion, OccurredAt: in.OccurredAt}
		if in.PositionMs != nil {
			event.PositionMs = *in.PositionMs
		} else if event.Kind == "progress" {
			return nil, 0, service.Fail(400, "missing_progress_position", "播放进度需要明确的毫秒位置。")
		}
		if in.SessionId != nil {
			event.SessionID = *in.SessionId
		}
		if in.Seconds != nil {
			event.Seconds = *in.Seconds
		}
		v, e := a.s.RecordEvent(r.Context(), w, id, event)
		return v, 200, e
	})
}

func (a *API) GetAssetContent(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.GetAssetContentParams) {
	if !allowScope(w, r, "read") {
		return
	}
	v, e := contentVersion(r)
	if e != nil {
		fail(w, e)
		return
	}
	f, asset, e := a.s.Content(r.Context(), id, v)
	if e != nil {
		fail(w, e)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", asset.MimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", `"`+asset.Hash+`"`)
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, asset.FileName, asset.UpdatedAt, f)
}

func (a *API) GetAssetThumbnail(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.GetAssetThumbnailParams) {
	if !allowScope(w, r, "read") {
		return
	}
	v, e := contentVersion(r)
	if e != nil {
		fail(w, e)
		return
	}
	background := params.Background != nil && *params.Background
	f, asset, e := a.s.ThumbnailWithPriority(r.Context(), id, v, r.URL.Query().Get("size"), background)
	if e != nil {
		fail(w, e)
		return
	}
	defer f.Close()
	hash := sha256.New()
	if _, e = io.Copy(hash, f); e != nil {
		fail(w, e)
		return
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		fail(w, e)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Qimeng-Instance-Id", a.s.InstanceID)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", `"thumb-`+hex.EncodeToString(hash.Sum(nil))+`"`)
	http.ServeContent(w, r, "thumbnail.jpg", asset.UpdatedAt, f)
}

func (a *API) ListThumbnailCandidates(w http.ResponseWriter, r *http.Request, params generated.ListThumbnailCandidatesParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.ThumbnailCandidates(r.Context(), r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) GetThumbnailProgress(w http.ResponseWriter, r *http.Request) {
	a.readScoped(w, r, "admin", func(r *http.Request) (any, error) {
		return a.s.ThumbnailProgress(r.Context())
	})
}

func (a *API) MoveAsset(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.MoveAssetParams) {
	write[struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		RelativePath     string `json:"relativePath"`
	}](a, w, r, true, func(r *http.Request, w *service.Write, in struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		RelativePath     string `json:"relativePath"`
	}) (any, int, error) {
		v, e := a.s.Move(r.Context(), w, id, in.RelativePath, in.ExpectedRevision)
		return v, 200, e
	})
}

func (a *API) TrashAsset(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.TrashAssetParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		rev, e := revision(r)
		if e != nil {
			return nil, 0, e
		}
		v, e := a.s.Trash(r.Context(), w, id, rev)
		return v, 200, e
	})
}

func (a *API) ListTags(w http.ResponseWriter, r *http.Request, params generated.ListTagsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		q := r.URL.Query()
		return a.s.Tags(r.Context(), q.Get("q"), q.Get("cursor"), n)
	})
}

func (a *API) ListHistory(w http.ResponseWriter, r *http.Request, params generated.ListHistoryParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		f, e := assetFilterRequest(r)
		if e != nil {
			return nil, e
		}
		return a.s.FilteredHistory(r.Context(), f)
	})
}

func (a *API) ListAuthors(w http.ResponseWriter, r *http.Request, params generated.ListAuthorsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		f, e := boolQuery(r, "following")
		if e != nil {
			return nil, e
		}
		return a.s.Authors(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("cursor"), n, f != nil && *f, r.URL.Query().Get("kind"), r.URL.Query().Get("sort"), r.URL.Query().Get("exactName"))
	})
}

func (a *API) GetAuthor(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.Author(r.Context(), id) })
}

func (a *API) CreateAuthor(w http.ResponseWriter, r *http.Request, params generated.CreateAuthorParams) {
	write[service.AuthorInput](a, w, r, true, func(r *http.Request, w *service.Write, in service.AuthorInput) (any, int, error) {
		v, e := a.s.CreateAuthor(r.Context(), w, in)
		return v, 201, e
	})
}

func (a *API) UpdateAuthor(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.UpdateAuthorParams) {
	write[service.AuthorUpdate](a, w, r, true, func(r *http.Request, w *service.Write, in service.AuthorUpdate) (any, int, error) {
		v, e := a.s.UpdateAuthor(r.Context(), w, id, in)
		return v, 200, e
	})
}

func (a *API) ListCollections(w http.ResponseWriter, r *http.Request, params generated.ListCollectionsParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		q := r.URL.Query()
		return a.s.Collections(r.Context(), q.Get("libraryId"), q.Get("q"), q.Get("cursor"), n)
	})
}

func (a *API) CreateCollection(w http.ResponseWriter, r *http.Request, params generated.CreateCollectionParams) {
	write[collectionCreateRequest](a, w, r, true, func(r *http.Request, w *service.Write, in collectionCreateRequest) (any, int, error) {
		v, e := a.s.CreateCollection(r.Context(), w, model.Collection{LibraryID: in.LibraryID, Name: in.Name, Kind: in.Kind, Aliases: in.Aliases})
		return v, 201, e
	})
}

func (a *API) ListTrash(w http.ResponseWriter, r *http.Request, params generated.ListTrashParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.Assets(r.Context(), service.AssetFilter{Trashed: true, Cursor: r.URL.Query().Get("cursor"), Limit: n})
	})
}

func (a *API) RestoreAsset(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.RestoreAssetParams) {
	write[revisionBody](a, w, r, true, func(r *http.Request, w *service.Write, in revisionBody) (any, int, error) {
		v, e := a.s.Restore(r.Context(), w, id, in.ExpectedRevision)
		return v, 200, e
	})
}

func (a *API) PurgeAsset(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.PurgeAssetParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		rev, e := revision(r)
		if e != nil {
			return nil, 0, e
		}
		return nil, 204, a.s.Purge(r.Context(), w, id, rev)
	})
}

func (a *API) GetStats(w http.ResponseWriter, r *http.Request) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.Stats(r.Context()) })
}

func (a *API) GetSettings(w http.ResponseWriter, r *http.Request) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.Settings(r.Context()) })
}

func (a *API) UpdateSettings(w http.ResponseWriter, r *http.Request, params generated.UpdateSettingsParams) {
	write[service.SettingsUpdate](a, w, r, true, func(r *http.Request, w *service.Write, in service.SettingsUpdate) (any, int, error) {
		v, e := a.s.UpdateSettings(r.Context(), w, in)
		return v, 200, e
	})
}

func (a *API) CreateBackupManifest(w http.ResponseWriter, r *http.Request, params generated.CreateBackupManifestParams) {
	write[struct{}](a, w, r, true, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		v, e := a.s.CreateBackup(r.Context(), w)
		return v, 201, e
	})
}

func (a *API) GetOperation(w http.ResponseWriter, r *http.Request, key string, params generated.GetOperationParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.Operation(r.Context(), principal(r), key, r.URL.Query().Get("operation"))
	})
}
func (a *API) CreateUpload(w http.ResponseWriter, r *http.Request, params generated.CreateUploadParams) {
	write[model.UploadInput](a, w, r, true, func(r *http.Request, w *service.Write, in model.UploadInput) (any, int, error) {
		v, e := a.s.CreateUpload(r.Context(), w, in)
		return v, 201, e
	})
}

func (a *API) GetUpload(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) { return a.s.Upload(r.Context(), principal(r), id) })
}

func (a *API) CancelUpload(w http.ResponseWriter, r *http.Request, id generated.Id) {
	write[struct{}](a, w, r, false, func(r *http.Request, w *service.Write, _ struct{}) (any, int, error) {
		return nil, 204, a.s.CancelUpload(r.Context(), w, id)
	})
}

func (a *API) AppendUpload(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.AppendUploadParams) {
	if !allowScope(w, r, "read") {
		return
	}
	offset := params.UploadOffset
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, service.MaxChunkBytes))
	if e != nil {
		fail(w, service.Fail(413, "chunk_too_large", "单个分片最多 8 MiB。"))
		return
	}
	out, e := a.s.Run(r.Context(), principal(r), "appendUpload:"+id, "", nil, func(tx *service.Write) (any, int, error) {
		v, e := a.s.AppendUpload(r.Context(), tx, id, offset, body)
		return v, 200, e
	})
	if e != nil {
		fail(w, e)
		return
	}
	result(w, out)
}

func (a *API) CompleteUpload(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.CompleteUploadParams) {
	a.completeUploadRequest(w, r, id)
}
func (a *API) ListBatches(w http.ResponseWriter, r *http.Request, params generated.ListBatchesParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.Batches(r.Context(), principal(r), r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) CreateBatch(w http.ResponseWriter, r *http.Request, params generated.CreateBatchParams) {
	write[batchCreateRequest](a, w, r, true, func(r *http.Request, w *service.Write, in batchCreateRequest) (any, int, error) {
		budget := 8
		if in.MaxResearchCalls != nil {
			budget = *in.MaxResearchCalls
		}
		v, e := a.s.CreateBatch(r.Context(), w, model.Batch{LibraryID: in.LibraryID, Mode: in.Mode, Label: in.Label, MaxResearchCalls: budget})
		return v, 201, e
	})
}

func (a *API) GetBatch(w http.ResponseWriter, r *http.Request, id generated.Id) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.BatchDetail(r.Context(), principal(r), id)
	})
}

func (a *API) SetBatchInputs(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.SetBatchInputsParams) {
	write[model.BatchInputsSet](a, w, r, true, func(r *http.Request, w *service.Write, in model.BatchInputsSet) (any, int, error) {
		v, e := a.s.SetBatchInputs(r.Context(), w, id, in)
		return v, 200, e
	})
}

func (a *API) CancelBatchInput(w http.ResponseWriter, r *http.Request, id generated.Id, inputId string, params generated.CancelBatchInputParams) {
	write[struct {
		ManifestFingerprint string `json:"manifestFingerprint"`
	}](a, w, r, true, func(r *http.Request, w *service.Write, in struct {
		ManifestFingerprint string `json:"manifestFingerprint"`
	}) (any, int, error) {
		v, e := a.s.CancelBatchInput(r.Context(), w, id, inputId, in.ManifestFingerprint)
		return v, 200, e
	})
}

func (a *API) CancelBatch(w http.ResponseWriter, r *http.Request, id generated.Id) {
	write[revisionBody](a, w, r, false, func(r *http.Request, w *service.Write, in revisionBody) (any, int, error) {
		v, e := a.s.CancelBatch(r.Context(), w, id, in.ExpectedRevision)
		return v, 200, e
	})
}

func (a *API) GetTaskContext(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.GetTaskContextParams) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		n, e := limit(r)
		if e != nil {
			return nil, e
		}
		return a.s.Context(r.Context(), principal(r), id, r.URL.Query().Get("cursor"), n)
	})
}

func (a *API) SubmitResearch(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.SubmitResearchParams) {
	write[model.Research](a, w, r, true, func(r *http.Request, w *service.Write, in model.Research) (any, int, error) {
		v, e := a.s.SubmitResearch(r.Context(), w, id, in)
		return v, 200, e
	})
}

func (a *API) PreparePlan(w http.ResponseWriter, r *http.Request, id generated.Id, params generated.PreparePlanParams) {
	write[model.PlanCreate](a, w, r, true, func(r *http.Request, w *service.Write, in model.PlanCreate) (any, int, error) {
		v, e := a.s.PreparePlan(r.Context(), w, id, in)
		return v, 201, e
	})
}

func (a *API) GetPlan(w http.ResponseWriter, r *http.Request, id generated.Id, planId generated.PlanId) {
	a.readJSON(w, r, func(r *http.Request) (any, error) {
		return a.s.Plan(r.Context(), principal(r), id, planId)
	})
}

func (a *API) CommitPlan(w http.ResponseWriter, r *http.Request, id generated.Id, planId generated.PlanId, params generated.CommitPlanParams) {
	if !allowScope(w, r, "read") {
		return
	}
	in, _, e := decode[struct {
		Fingerprint string `json:"fingerprint"`
	}](w, r)
	if e != nil {
		fail(w, e)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 8 || len(key) > 128 {
		fail(w, service.Fail(400, "idempotency_required", "提交计划须提供稳定幂等键。"))
		return
	}
	out, e := a.s.CommitPlan(r.Context(), principal(r), id, planId, in.Fingerprint, key)
	if e != nil {
		fail(w, e)
		return
	}
	result(w, out)
}
