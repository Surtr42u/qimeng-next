package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"time"

	"qimeng.local/next/server/internal/domain"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func (s *Service) CosWorks(ctx context.Context, libraryID, authorID, query, cursor string, limit int) (model.Page[model.CosWork], error) {
	page := model.Page[model.CosWork]{Items: []model.CosWork{}}
	n, e := limited(limit, 50)
	if e != nil {
		return page, e
	}
	keyRaw, _ := json.Marshal([]string{libraryID, authorID, query})
	key := string(keyRaw)
	seek, e := readSeek(cursor, key, "name")
	if e != nil {
		return page, e
	}
	items, e := store.List[model.CosWork](ctx, s.DB.DB, "SELECT data FROM cos_works WHERE (?='' OR library_id=?) AND (?='' OR author_id=?) AND instr(lower(name),lower(?))>0 AND (?='' OR (name,id)>(?,?)) ORDER BY name,id LIMIT ?", libraryID, libraryID, authorID, authorID, query, seek.ID, seek.Value, seek.ID, n+1)
	if e != nil {
		return page, e
	}
	page.Items = items
	if len(items) > n {
		page.Items = items[:n]
		last := page.Items[n-1]
		page.NextCursor = makeSeek(key, "name", last.Name, last.ID)
	}
	return page, nil
}

func (s *Service) planCOSPlacement(ctx context.Context, q store.Querier, a *model.Asset) (*model.Author, *model.CosWork, error) {
	library, e := store.Get[model.Library](ctx, q, "SELECT data FROM libraries WHERE id=?", a.LibraryID)
	if e != nil {
		return nil, nil, e
	}
	if library.Kind != "cos" {
		return nil, nil, nil
	}
	if !domain.ValidMediaPath(a.RelativePath) {
		return nil, nil, Fail(400, "invalid_path", "COS 目录路径无效。")
	}
	if e = s.safe(path.Join("libraries", a.LibraryID, path.Dir(a.RelativePath))); e != nil {
		return nil, nil, e
	}
	parts := strings.Split(a.RelativePath, "/")
	directoryAuthor, e := store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id='unknown'")
	if e != nil {
		return nil, nil, e
	}
	var createdAuthor *model.Author
	if len(parts) > 1 && parts[0] != directoryAuthor.Name {
		matches, e := store.List[model.Author](ctx, q, "SELECT data FROM authors WHERE name=? AND json_extract(data,'$.kind')='cos' ORDER BY id LIMIT 2", parts[0])
		if e != nil {
			return nil, nil, e
		}
		if len(matches) > 1 {
			return nil, nil, Fail(409, "ambiguous_cos_directory_author", "COS 目录对应多个作者身份，请先核对。")
		}
		if len(matches) == 1 {
			directoryAuthor = matches[0]
		} else {
			pending, e := store.List[model.Author](ctx, q, "SELECT json_extract(data,'$.cosAuthor') FROM file_intents WHERE state='prepared' AND json_extract(data,'$.cosAuthor.name')=? LIMIT 2", parts[0])
			if e != nil {
				return nil, nil, e
			}
			if len(pending) > 1 && pending[0].ID != pending[1].ID {
				return nil, nil, Fail(409, "ambiguous_cos_directory_author", "目录作者有未决身份，请先恢复原文件操作。")
			}
			if len(pending) > 0 {
				directoryAuthor = pending[0]
			} else {
				now := time.Now().UTC()
				directoryAuthor = model.Author{ID: digest([]byte("cos-directory:" + a.LibraryID + ":" + parts[0]))[:32], Name: parts[0], Kind: "cos", Aliases: []string{}, Revision: 1, FieldStatus: map[string]string{"identity": "candidate", "biography": "missing", "homepage": "missing"}, CreatedAt: now, UpdatedAt: now}
			}
			createdAuthor = &directoryAuthor
		}
	}
	previous := a.COSDirectoryAuthorID
	if previous == "" && a.MetadataSource == "scanner" && len(a.AuthorIDs) == 1 {
		previous = a.AuthorIDs[0]
	}
	desired := []model.Author{}
	if len(parts) > 1 && !directoryAuthor.Placeholder {
		desired = append(desired, directoryAuthor)
	}
	for _, id := range a.AuthorIDs {
		if id == previous || id == "unknown" || id == directoryAuthor.ID {
			continue
		}
		author, e := store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id=?", id)
		if e != nil {
			return nil, nil, e
		}
		desired = append(desired, author)
	}
	if len(desired) == 0 {
		unknown, e := store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id='unknown'")
		if e != nil {
			return nil, nil, e
		}
		desired = append(desired, unknown)
	}
	ids := []string{}
	for _, author := range desired {
		ids = append(ids, author.ID)
	}
	if strings.Join(ids, "\x00") != strings.Join(a.AuthorIDs, "\x00") {
		setAuthors(a, desired)
		a.AuthorsRevision++
	}
	a.COSDirectoryAuthorID = ""
	if len(parts) > 1 {
		a.COSDirectoryAuthorID = directoryAuthor.ID
	}
	a.CosWorkID = ""
	if len(parts) < 3 {
		return createdAuthor, nil, nil
	}
	directory := path.Join(parts[0], parts[1])
	existing, e := store.Get[model.CosWork](ctx, q, "SELECT data FROM cos_works WHERE library_id=? AND relative_directory=?", a.LibraryID, directory)
	if e == nil {
		if existing.AuthorID != directoryAuthor.ID {
			return nil, nil, Fail(409, "cos_work_author_conflict", "COS 作品目录的已登记作者身份发生冲突。")
		}
		a.CosWorkID = existing.ID
		return createdAuthor, nil, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, nil, e
	}
	work := model.CosWork{ID: digest([]byte("cos-work:" + a.LibraryID + ":" + directory))[:32], LibraryID: a.LibraryID, AuthorID: directoryAuthor.ID, RelativeDirectory: directory, Name: parts[1], Revision: 1, CreatedAt: time.Now().UTC()}
	a.CosWorkID = work.ID
	return createdAuthor, &work, nil
}
func saveCOSPlacement(ctx context.Context, q store.Querier, author *model.Author, work *model.CosWork) error {
	if author != nil {
		var exists int
		if e := q.QueryRowContext(ctx, "SELECT count(*) FROM authors WHERE name=? AND json_extract(data,'$.kind')='cos' AND id!=?", author.Name, author.ID).Scan(&exists); e != nil {
			return e
		}
		if exists > 0 {
			return Fail(409, "cos_author_changed", "目录作者身份已改变，原操作需要核对。")
		}
		raw, e := store.Encode(author)
		if e != nil {
			return e
		}
		if _, e = q.ExecContext(ctx, "INSERT OR IGNORE INTO authors(id,name,following,data) VALUES(?,?,0,?)", author.ID, author.Name, raw); e != nil {
			return e
		}
	}
	if work != nil {
		raw, e := store.Encode(work)
		if e != nil {
			return e
		}
		if _, e = q.ExecContext(ctx, "INSERT OR IGNORE INTO cos_works(id,library_id,author_id,relative_directory,name,data) VALUES(?,?,?,?,?,?)", work.ID, work.LibraryID, work.AuthorID, work.RelativeDirectory, work.Name, raw); e != nil {
			return e
		}
		var id string
		if e = q.QueryRowContext(ctx, "SELECT id FROM cos_works WHERE library_id=? AND relative_directory=?", work.LibraryID, work.RelativeDirectory).Scan(&id); e != nil {
			return e
		}
		if id != work.ID {
			return Fail(409, "cos_work_changed", "COS 作品目录身份已改变，原操作需要核对。")
		}
	}
	return nil
}

func (s *Service) prepareCOSUpload(ctx context.Context, in *model.UploadInput) error {
	if in.CosWorkID == "" {
		return nil
	}
	work, e := store.Get[model.CosWork](ctx, s.DB.DB, "SELECT data FROM cos_works WHERE id=?", in.CosWorkID)
	if e != nil {
		return Normalize(e)
	}
	if work.LibraryID != in.LibraryID {
		return Fail(409, "cos_work_library_conflict", "COS 作品属于其他库。")
	}
	if in.RelativeDirectory != "" && in.RelativeDirectory != work.RelativeDirectory {
		return Fail(409, "cos_work_directory_conflict", "显式目录与已登记 COS 作品不一致。")
	}
	in.RelativeDirectory = work.RelativeDirectory
	chosen := in.AuthorIDs
	if len(chosen) == 0 && in.AuthorID != "" {
		chosen = []string{in.AuthorID}
	}
	in.AuthorIDs = []string{work.AuthorID}
	for _, id := range chosen {
		if id == "unknown" || id == work.AuthorID {
			continue
		}
		author, e := store.Get[model.Author](ctx, s.DB.DB, "SELECT data FROM authors WHERE id=?", id)
		if e != nil {
			return Normalize(e)
		}
		if author.Kind != "regular" {
			return Fail(409, "cos_work_author_conflict", "不能将其他目录的 COS 身份附加到此作品。")
		}
		in.AuthorIDs = appendUnique(in.AuthorIDs, id)
	}
	return nil
}
