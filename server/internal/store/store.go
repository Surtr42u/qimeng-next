// Package store owns SQLite schema and record persistence. Only the service owns writes.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"

	"qimeng.local/next/server/internal/model"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ DB *sql.DB }

// FileURI follows SQLite's Windows drive-letter rule (/C:/...) and escapes
// literal query/fragment characters before callers append connection options.
func FileURI(filename string) string {
	p := filepath.ToSlash(filename)
	if len(p) >= 2 && p[1] == ':' {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p, OmitHost: true}).String()
}

func Open(path string) (*Store, error) {
	db, err := OpenDatabase(path, CatalogDatabase)
	if err != nil {
		return nil, err
	}
	if err := applySchema(db, migrations); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return &Store{DB: db}, nil
}

type Querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func Encode(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func Get[T any](ctx context.Context, q Querier, query string, args ...any) (T, error) {
	var value T
	var raw string
	if err := q.QueryRowContext(ctx, query, args...).Scan(&raw); err != nil {
		return value, err
	}
	err := json.Unmarshal([]byte(raw), &value)
	return value, err
}
func List[T any](ctx context.Context, q Querier, query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []T{}
	for rows.Next() {
		var raw string
		var v T
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}
func SaveAsset(ctx context.Context, q Querier, a model.Asset) error {
	return writeAsset(ctx, q, a, false)
}

// InsertAsset requires a fresh identity. A collision fails before any relations
// are written. Foreign keys guarantee that a new asset has no old relations.
func InsertAsset(ctx context.Context, q Querier, a model.Asset) error {
	return writeAsset(ctx, q, a, true)
}

func writeAsset(ctx context.Context, q Querier, a model.Asset, insertOnly bool) error {
	if db, ok := q.(*sql.DB); ok {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = writeAsset(ctx, tx, a, insertOnly); err != nil {
			return err
		}
		return tx.Commit()
	}
	a.FileDate = a.EffectiveFileDate()
	newAsset := insertOnly
	if !insertOnly {
		if err := q.QueryRowContext(ctx, "SELECT NOT EXISTS(SELECT 1 FROM asset_records WHERE id=?)", a.ID).Scan(&newAsset); err != nil {
			return err
		}
	}
	raw, err := Encode(a)
	if err != nil {
		return err
	}
	var fileDate, favoritedAt, favoriteUpdatedAt any
	if a.FileDate != nil {
		fileDate = a.FileDate.UnixMilli()
	}
	if a.Favorite && a.FavoritedAt != nil {
		favoritedAt = a.FavoritedAt.UTC().Format("2006-01-02T15:04:05.000000000Z")
	}
	if a.FavoriteUpdatedAt != nil {
		favoriteUpdatedAt = a.FavoriteUpdatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z")
	}
	query := `INSERT INTO asset_records(id,author_id,revision,created_at,updated_at,favorite,favorite_updated_at,
		data,file_date,favorited_at,cos_work_id) VALUES(?,?,?,?,?,?,?,json_remove(?,
		'$.id','$.hash','$.size','$.contentVersion','$.mediaType','$.mimeType','$.width','$.height','$.durationMs','$.probeState',
		'$.libraryId','$.relativePath','$.fileName','$.contentMtime','$.storageLayoutVersion','$.trashed','$.positionMs','$.positionContentVersion',
		'$.tags','$.authorId','$.authorIds','$.authorName','$.favorite','$.favoriteUpdatedAt','$.favoritedAt','$.liked','$.likeCount','$.unattributedLikeCount',
		'$.collectionId','$.page','$.source','$.sourceIds','$.characters','$.characterIds','$.taxonomyDerived','$.taxonomyCandidates',
		'$.workKeys','$.roleKeys','$.workFacets','$.roleFacets'),?,?,?)`
	if !insertOnly {
		query += ` ON CONFLICT(id) DO UPDATE SET author_id=excluded.author_id,
			revision=excluded.revision,updated_at=excluded.updated_at,data=excluded.data,file_date=excluded.file_date,cos_work_id=excluded.cos_work_id`
	}
	_, err = q.ExecContext(ctx, query, a.ID, a.AuthorID, a.Revision, a.CreatedAt.Format("2006-01-02T15:04:05.000000000Z"),
		a.UpdatedAt.Format("2006-01-02T15:04:05.000000000Z"), a.Favorite, favoriteUpdatedAt, raw, fileDate, favoritedAt, a.CosWorkID)
	if err != nil {
		return err
	}
	if err = saveAssetFile(ctx, q, a, insertOnly); err != nil {
		return err
	}
	if a.CollectionID != "" {
		// An existing member owns its position through its own revision.
		// Publishing or moving a file must not replay an older position.
		if _, err = q.ExecContext(ctx, `INSERT INTO collection_members(asset_id,collection_id,position,revision)
			SELECT ?,?,nullif(?,0),1 WHERE NOT EXISTS(SELECT 1 FROM collection_members WHERE asset_id=?)`,
			a.ID, a.CollectionID, a.Page, a.ID); err != nil {
			return err
		}
	}
	if err = saveAssetIDs(ctx, q, a.ID, "asset_authors", "author_id", a.AuthorIDs, insertOnly); err != nil {
		return err
	}
	if err = writeAssetTags(ctx, q, a, !insertOnly); err != nil {
		return err
	}
	if newAsset {
		return SaveAssetTaxonomy(ctx, q, a, CompleteTaxonomy(a))
	}
	return nil
}

// Callers supply literal table/column pairs. Keeping the same membership
// leaves its row in place, including when a generic file operation is replayed.
func saveAssetIDs(ctx context.Context, q Querier, assetID, table, column string, ids []string, insertOnly bool) error {
	if insertOnly && len(ids) == 0 {
		return nil
	}
	raw, err := Encode(ids)
	if err != nil {
		return err
	}
	if !insertOnly {
		if _, err = q.ExecContext(ctx, "DELETE FROM "+table+" WHERE asset_id=? AND NOT EXISTS(SELECT 1 FROM json_each(?) j WHERE j.value="+column+")", assetID, raw); err != nil {
			return err
		}
	}
	_, err = q.ExecContext(ctx, "INSERT INTO "+table+"(asset_id,"+column+",ordinal) SELECT ?,j.value,CAST(j.key AS INTEGER)"+
		" FROM json_each(?) j WHERE j.type='text' ON CONFLICT(asset_id,"+column+") DO UPDATE SET ordinal=excluded.ordinal WHERE ordinal!=excluded.ordinal", assetID, raw)
	return err
}
func SaveUpload(ctx context.Context, q Querier, u model.Upload) error {
	raw, err := Encode(u)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, "INSERT INTO uploads(id,batch_id,state,data) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,data=excluded.data", u.ID, u.BatchID, u.State, raw)
	return err
}
func SaveBatch(ctx context.Context, q Querier, b model.Batch) error {
	raw, err := Encode(b)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, "INSERT INTO batches(id,created_at,data) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", b.ID, b.CreatedAt.Format("2006-01-02T15:04:05.000000000Z"), raw)
	return err
}
func SavePlan(ctx context.Context, q Querier, p model.Plan) error {
	raw, err := Encode(p)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, "INSERT INTO plans(id,batch_id,data) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", p.ID, p.BatchID, raw)
	return err
}
