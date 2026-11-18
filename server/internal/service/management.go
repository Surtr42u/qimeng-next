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
	e = s.DB.DB.QueryRowContext(ctx, "SELECT COUNT(*)"+liveDirectoryRows+assetParentDir+"=?", libraryID, relative).Scan(&dir.FileCount)
	return dir, e
}

type Tag struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Revision  int64  `json:"revision"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

const tagJSON = "json_object('id',id,'name',name,'revision',revision,'createdAt',created_at,'updatedAt',updated_at)"

func (s *Service) TagVocabulary(ctx context.Context, cursor string, limit int) (model.Page[Tag], error) {
	n, e := limited(limit, 100)
	if e != nil {
		return model.Page[Tag]{}, e
	}
	o, e := pageOffset(cursor, "tag-vocabulary")
	if e != nil {
		return model.Page[Tag]{}, e
	}
	a, e := store.List[Tag](ctx, s.DB.DB, "SELECT "+tagJSON+" FROM tags WHERE deleted=0 ORDER BY name LIMIT ? OFFSET ?", n+1, o)
	return paged(a, n, o, "tag-vocabulary"), e
}

// A request identifies its spelling as well as its revision. Crossing the
// last revision for that spelling prevents stale forms editing a new owner.
const nextTagRevision = `SELECT coalesce(max(revision),0)+1 FROM
 (SELECT revision FROM tags WHERE name=?1 UNION ALL SELECT revision FROM retired_tag_names WHERE name=?1)`

func (s *Service) CreateTag(ctx context.Context, w *Write, name string) (Tag, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return Tag{}, e
	}
	names, e := cleanStrings([]string{name}, 1, 100)
	if e != nil {
		return Tag{}, e
	}
	name = names[0]
	q, e := w.Q()
	if e != nil {
		return Tag{}, e
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var existing string
	var deleted bool
	e = q.QueryRowContext(ctx, "SELECT id,deleted FROM tags WHERE name=? ORDER BY deleted,revision DESC,id LIMIT 1", name).Scan(&existing, &deleted)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return Tag{}, e
	}
	if e == nil && !deleted {
		return Tag{}, Fail(409, "tag_exists", "标签已存在。")
	}
	var revision int64
	if e = q.QueryRowContext(ctx, nextTagRevision, name).Scan(&revision); e != nil {
		return Tag{}, e
	}
	if existing == "" {
		return store.Get[Tag](ctx, q, "INSERT INTO tags(name,revision,created_at,updated_at) VALUES(?,?,?,?) RETURNING "+tagJSON, name, revision, now, now)
	}
	return store.Get[Tag](ctx, q, "UPDATE tags SET deleted=0,revision=?,updated_at=? WHERE id=? RETURNING "+tagJSON, revision, now, existing)
}

func (s *Service) ChangeTag(ctx context.Context, w *Write, name, newName string, revision int64, remove bool) (any, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return nil, e
	}
	q, e := w.Q()
	if e != nil {
		return nil, e
	}
	tag, e := store.Get[Tag](ctx, q, "SELECT "+tagJSON+" FROM tags WHERE name=? AND deleted=0", name)
	if e != nil {
		return nil, Normalize(e)
	}
	if revision != tag.Revision {
		return nil, Conflict(tag.Revision)
	}
	newRevision := tag.Revision + 1
	if !remove {
		v, e := cleanStrings([]string{newName}, 1, 100)
		if e != nil {
			return nil, e
		}
		newName = v[0]
		if newName == name {
			return tag, nil
		}
		var exists bool
		if e = q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM tags WHERE name=? AND deleted=0)", newName).Scan(&exists); e != nil {
			return nil, e
		}
		if exists {
			return nil, Fail(409, "tag_exists", "目标标签已存在，不会隐式合并。")
		}
		var next int64
		if e = q.QueryRowContext(ctx, nextTagRevision, newName).Scan(&next); e != nil {
			return nil, e
		}
		newRevision = max(newRevision, next)
	}
	now := time.Now().UTC()
	if _, e = q.ExecContext(ctx, `UPDATE asset_records SET revision=revision+1,updated_at=?,
		data=json_set(data,'$.revision',revision+1,'$.updatedAt',?,'$.tagsRevision',coalesce(json_extract(data,'$.tagsRevision'),0)+1,
		'$.tagSetUpdatedAt',?,'$.tagSetSource','human') WHERE id IN (SELECT asset_id FROM asset_tags WHERE tag_id=?)`,
		dateKey(now), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), tag.ID); e != nil {
		return nil, e
	}
	if remove {
		if _, e = q.ExecContext(ctx, "DELETE FROM asset_tags WHERE tag_id=?", tag.ID); e != nil {
			return nil, e
		}
		_, e = q.ExecContext(ctx, "UPDATE tags SET deleted=1,revision=?,updated_at=? WHERE id=?", newRevision, now.Format(time.RFC3339Nano), tag.ID)
		return nil, e
	}
	if _, e = q.ExecContext(ctx, `INSERT INTO retired_tag_names(name,revision) VALUES(?,?)
		ON CONFLICT(name) DO UPDATE SET revision=max(revision,excluded.revision)`, name, tag.Revision+1); e != nil {
		return nil, e
	}
	return store.Get[Tag](ctx, q, "UPDATE tags SET name=?,revision=?,updated_at=? WHERE id=? RETURNING "+tagJSON,
		newName, newRevision, now.Format(time.RFC3339Nano), tag.ID)
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

func validTimelineValues(position int64, label, color string, duration int64) bool {
	return position >= 0 && (duration <= 0 || position <= duration) && strings.TrimSpace(label) != "" && len(label) <= 200 && (color == "" || hexColor.MatchString(color))
}

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
	if !validTimelineValues(in.PositionMs, in.Label, in.Color, a.DurationMs) {
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
	q, e := w.Q()
	if e != nil {
		return m, e
	}
	return m, writeTimelineMark(ctx, q, m)
}

func writeTimelineMark(ctx context.Context, q store.Querier, m TimelineMark) error {
	raw, err := store.Encode(m)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, "INSERT INTO timeline_marks(id,asset_id,position_ms,data) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET position_ms=excluded.position_ms,data=excluded.data", m.ID, m.AssetID, m.PositionMs, raw)
	return err
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
