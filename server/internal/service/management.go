package service

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"regexp"
	"strings"
	"time"

	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type LibraryUpdate struct {
	ExpectedRevision int64   `json:"expectedRevision"`
	Name             *string `json:"name"`
	Enabled          *bool   `json:"enabled"`
}

func (s *Service) UpdateLibrary(ctx context.Context, w *Write, id string, in LibraryUpdate) (model.Library, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return model.Library{}, e
	}
	l, e := s.library(ctx, id)
	if e != nil {
		return l, e
	}
	if l.Revision != in.ExpectedRevision {
		return l, Conflict(l.Revision)
	}
	if in.Enabled != nil && *in.Enabled != l.Enabled {
		w.after = append(w.after, func() error {
			if !*in.Enabled {
				s.stopLibraryScans(id)
				s.researchGateway.cancelSubject("", id)
			}
			s.wakeLibraryMonitor()
			s.wakeScans()
			return nil
		})
		if !*in.Enabled {
			q, e := w.Q()
			if e != nil {
				return l, e
			}
			if _, e = q.ExecContext(ctx, `UPDATE scans SET state='cancelled',data=json_set(data,'$.state','cancelled','$.updatedAt',?) WHERE library_id=? AND state IN ('queued','running') AND json_extract(data,'$.trigger') IN ('watch','poll')`, dateKey(time.Now().UTC()), id); e != nil {
				return l, e
			}
		}
	}
	if in.Name != nil {
		v := strings.TrimSpace(*in.Name)
		if v == "" || len(v) > 120 {
			return l, Fail(400, "invalid_library", "库名称无效。")
		}
		l.Name = v
	}
	if in.Enabled != nil {
		l.Enabled = *in.Enabled
		if l.Enabled {
			w.after = append(w.after, func() error { s.previews.changed(id, ""); return nil })
		}
	}
	l.Revision++
	raw, e := store.Encode(l)
	if e != nil {
		return l, e
	}
	q, e := w.Q()
	if e != nil {
		return l, e
	}
	if _, e = q.ExecContext(ctx, "UPDATE libraries SET name=?,data=? WHERE id=?", l.Name, raw, l.ID); e != nil {
		return l, e
	}
	return store.Get[model.Library](ctx, q, libraryProjection+"WHERE l.id=?", id)
}
func (s *Service) CreateDirectory(ctx context.Context, w *Write, libraryID, relative string) (Directory, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return Directory{}, e
	}
	l, e := s.library(ctx, libraryID)
	if e != nil {
		return Directory{}, e
	}
	if !l.Enabled {
		return Directory{}, Fail(409, "library_disabled", "目标库已停用。")
	}
	if relative == "" || !domain.ValidMediaPath(relative) {
		return Directory{}, Fail(400, "invalid_path", "目录路径无效。")
	}
	full := "libraries/" + libraryID + "/" + relative
	if e = s.safe(full); e != nil {
		return Directory{}, e
	}
	if e = s.Root.MkdirAll(full, 0700); e != nil {
		return Directory{}, e
	}
	if e = s.syncParent(full); e != nil {
		return Directory{}, e
	}
	dir := Directory{RelativePath: relative, Name: path.Base(relative)}
	e = s.DB.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM assets WHERE library_id=? AND trashed=0 AND "+assetParentDir+"=?", libraryID, relative).Scan(&dir.FileCount)
	return dir, e
}

type Tag struct {
	Name      string `json:"name"`
	Revision  int64  `json:"revision"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func (s *Service) TagVocabulary(ctx context.Context, cursor string, limit int) (model.Page[Tag], error) {
	n, e := limited(limit, 100)
	if e != nil {
		return model.Page[Tag]{}, e
	}
	o, e := pageOffset(cursor, "tag-vocabulary")
	if e != nil {
		return model.Page[Tag]{}, e
	}
	a, e := store.List[Tag](ctx, s.DB.DB, "SELECT json_object('name',name,'revision',revision,'createdAt',created_at,'updatedAt',updated_at) FROM tags WHERE deleted=0 ORDER BY name LIMIT ? OFFSET ?", n+1, o)
	return paged(a, n, o, "tag-vocabulary"), e
}
func (s *Service) CreateTag(ctx context.Context, w *Write, name string) (Tag, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return Tag{}, e
	}
	names, e := cleanStrings([]string{name}, 1, 100)
	if e != nil {
		return Tag{}, e
	}
	name = names[0]
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var revision int64
	var deleted bool
	e = s.DB.DB.QueryRowContext(ctx, "SELECT revision,deleted FROM tags WHERE name=?", name).Scan(&revision, &deleted)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return Tag{}, e
	}
	if e == nil && !deleted {
		return Tag{}, Fail(409, "tag_exists", "标签已存在。")
	}
	tag := Tag{Name: name, Revision: revision + 1, CreatedAt: now, UpdatedAt: now}
	q, e := w.Q()
	if e != nil {
		return tag, e
	}
	_, e = q.ExecContext(ctx, "INSERT INTO tags(name,revision,created_at,updated_at) VALUES(?,?,?,?) ON CONFLICT(name) DO UPDATE SET revision=excluded.revision,deleted=0,updated_at=excluded.updated_at", name, tag.Revision, now, now)
	return tag, e
}
func (s *Service) ChangeTag(ctx context.Context, w *Write, name, newName string, revision int64, remove bool) (any, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return nil, e
	}
	var tag Tag
	e := s.DB.DB.QueryRowContext(ctx, "SELECT name,revision,created_at,updated_at FROM tags WHERE name=? AND deleted=0", name).Scan(&tag.Name, &tag.Revision, &tag.CreatedAt, &tag.UpdatedAt)
	if e != nil {
		return nil, Normalize(e)
	}
	if revision != tag.Revision {
		return nil, Conflict(tag.Revision)
	}
	if !remove {
		v, e := cleanStrings([]string{newName}, 1, 100)
		if e != nil {
			return nil, e
		}
		newName = v[0]
		if newName == name {
			return tag, nil
		}
		var exists int
		if e = s.DB.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM tags WHERE name=? AND deleted=0", newName).Scan(&exists); e != nil {
			return nil, e
		}
		if exists > 0 {
			return nil, Fail(409, "tag_exists", "目标标签已存在，不会隐式合并。")
		}
	}
	assets, e := store.List[model.Asset](ctx, s.DB.DB, "SELECT a.data FROM assets a JOIN asset_tags t ON t.asset_id=a.id WHERE t.tag=?", name)
	if e != nil {
		return nil, e
	}
	q, e := w.Q()
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	if !remove {
		if _, e = q.ExecContext(ctx, "INSERT INTO tags(name,revision,created_at,updated_at) VALUES(?,1,?,?) ON CONFLICT(name) DO UPDATE SET deleted=0,revision=tags.revision+1,updated_at=excluded.updated_at WHERE tags.deleted=1", newName, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); e != nil {
			return nil, e
		}
	}
	for _, a := range assets {
		tags := []string{}
		for _, v := range a.Tags {
			if v != name {
				tags = append(tags, v)
			} else if !remove {
				tags = append(tags, newName)
			}
		}
		a.Tags = tags
		a.TagsRevision++
		a.Revision++
		a.UpdatedAt = now
		a.TagSetUpdatedAt = &now
		a.TagSetSource = "human"
		if e = store.SaveAssetTags(ctx, q, a); e != nil {
			return nil, e
		}
	}
	if _, e = q.ExecContext(ctx, "UPDATE tags SET deleted=1,revision=revision+1,updated_at=? WHERE name=?", now.Format(time.RFC3339Nano), name); e != nil {
		return nil, e
	}
	if remove {
		return nil, nil
	}
	var result Tag
	e = q.QueryRowContext(ctx, "SELECT name,revision,created_at,updated_at FROM tags WHERE name=?", newName).Scan(&result.Name, &result.Revision, &result.CreatedAt, &result.UpdatedAt)
	if errors.Is(e, sql.ErrNoRows) {
		result = Tag{Name: newName, Revision: 1, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
		_, e = q.ExecContext(ctx, "INSERT INTO tags(name,revision,created_at,updated_at) VALUES(?,1,?,?)", newName, result.CreatedAt, result.UpdatedAt)
	}
	return result, e
}
func (s *Service) RemoveAssetTag(ctx context.Context, w *Write, id, tag string, revision int64) (model.Asset, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return model.Asset{}, e
	}
	a, e := s.Asset(ctx, id)
	if e != nil {
		return a, e
	}
	if a.TagsRevision != revision {
		return a, Conflict(a.TagsRevision)
	}
	found := false
	tags := []string{}
	for _, v := range a.Tags {
		if v == tag {
			found = true
		} else {
			tags = append(tags, v)
		}
	}
	if !found {
		return a, nil
	}
	a.Tags = tags
	a.TagsRevision++
	a.Revision++
	a.UpdatedAt = time.Now().UTC()
	a.TagSetUpdatedAt = &a.UpdatedAt
	a.TagSetSource = "human"
	q, e := w.Q()
	if e != nil {
		return a, e
	}
	return a, store.SaveAssetTags(ctx, q, a)
}

type TimelineMark struct {
	ID             string    `json:"id"`
	AssetID        string    `json:"assetId"`
	PositionMs     int64     `json:"positionMs"`
	Label          string    `json:"label"`
	Color          string    `json:"color,omitempty"`
	ContentVersion int64     `json:"contentVersion"`
	Revision       int64     `json:"revision"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}
type TimelineInput struct {
	PositionMs       int64  `json:"positionMs"`
	Label            string `json:"label"`
	Color            string `json:"color,omitempty"`
	ContentVersion   int64  `json:"contentVersion"`
	ExpectedRevision int64  `json:"expectedRevision,omitempty"`
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (s *Service) Timeline(ctx context.Context, assetID, cursor string, limit int) (model.Page[TimelineMark], error) {
	n, e := limited(limit, 100)
	if e != nil {
		return model.Page[TimelineMark]{}, e
	}
	if _, e = s.Asset(ctx, assetID); e != nil {
		return model.Page[TimelineMark]{}, e
	}
	key := "timeline:" + assetID
	o, e := pageOffset(cursor, key)
	if e != nil {
		return model.Page[TimelineMark]{}, e
	}
	a, e := store.List[TimelineMark](ctx, s.DB.DB, "SELECT data FROM timeline_marks WHERE asset_id=? ORDER BY position_ms,id LIMIT ? OFFSET ?", assetID, n+1, o)
	return paged(a, n, o, key), e
}
func (s *Service) SaveMark(ctx context.Context, w *Write, assetID, markID string, in TimelineInput) (TimelineMark, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return TimelineMark{}, e
	}
	a, e := s.Asset(ctx, assetID)
	if e != nil {
		return TimelineMark{}, e
	}
	if a.MediaType != "video" || a.Trashed {
		return TimelineMark{}, Fail(400, "video_required", "时间轴标记只用于可用视频。")
	}
	if a.ContentVersion != in.ContentVersion {
		return TimelineMark{}, Fail(409, "content_changed", "视频内容已变化，旧标记不会自动映射。")
	}
	if in.PositionMs < 0 || (a.DurationMs > 0 && in.PositionMs > a.DurationMs) || strings.TrimSpace(in.Label) == "" || len(in.Label) > 200 || (in.Color != "" && !hexColor.MatchString(in.Color)) {
		return TimelineMark{}, Fail(400, "invalid_mark", "标记时间、名称或颜色无效。")
	}
	now := time.Now().UTC()
	m := TimelineMark{ID: w.ID("mark"), AssetID: assetID, PositionMs: in.PositionMs, Label: in.Label, Color: in.Color, ContentVersion: in.ContentVersion, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if markID != "" {
		old, e := store.Get[TimelineMark](ctx, s.DB.DB, "SELECT data FROM timeline_marks WHERE id=? AND asset_id=?", markID, assetID)
		if e != nil {
			return m, Normalize(e)
		}
		if old.Revision != in.ExpectedRevision {
			return m, Conflict(old.Revision)
		}
		if old.ContentVersion != in.ContentVersion {
			return m, Fail(409, "content_changed", "标记属于之前的视频版本；可为当前版本新建标记。")
		}
		m.ID = old.ID
		m.Revision = old.Revision + 1
		m.CreatedAt = old.CreatedAt
	}
	raw, e := store.Encode(m)
	if e != nil {
		return m, e
	}
	q, e := w.Q()
	if e != nil {
		return m, e
	}
	_, e = q.ExecContext(ctx, "INSERT INTO timeline_marks(id,asset_id,position_ms,data) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET position_ms=excluded.position_ms,data=excluded.data", m.ID, assetID, m.PositionMs, raw)
	return m, e
}
func (s *Service) DeleteMark(ctx context.Context, w *Write, assetID, id string, revision int64) error {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return e
	}
	m, e := store.Get[TimelineMark](ctx, s.DB.DB, "SELECT data FROM timeline_marks WHERE id=? AND asset_id=?", id, assetID)
	if e != nil {
		return Normalize(e)
	}
	if m.Revision != revision {
		return Conflict(m.Revision)
	}
	q, e := w.Q()
	if e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, "DELETE FROM timeline_marks WHERE id=?", id)
	return e
}
