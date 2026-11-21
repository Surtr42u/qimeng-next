package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

type BackupChangeState struct {
	InstanceID       string `json:"instanceId"`
	BusinessRevision string `json:"businessRevision"`
}

func (s *Service) BackupChangeState(ctx context.Context, principal model.Principal) (BackupChangeState, error) {
	state := BackupChangeState{InstanceID: s.InstanceID}
	if err := Authorize(principal, "admin", ""); err != nil {
		return state, err
	}
	err := s.DB.DB.QueryRowContext(ctx, "SELECT revision FROM backup_business_state WHERE id=1").Scan(&state.BusinessRevision)
	return state, err
}

var authorDocumentSlots = make(chan struct{}, 2)
var markdownLiteral = strings.NewReplacer("\\", "\\\\", "&", "&amp;", "<", "&lt;", ">", "&gt;", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "`", "\\`", "#", "\\#", "!", "\\!", "\r", "", "\n", "\n    ")

func documentField(out io.Writer, label, value string) error {
	if value == "" {
		return nil
	}
	_, err := fmt.Fprintf(out, "- %s　%s\n", label, markdownLiteral.Replace(value))
	return err
}

func documentProfile(out io.Writer, block AuthorProfileBlock) error {
	values := block.Values
	for _, field := range []struct {
		label string
		value *string
	}{
		{"公开名称", values.PublicName}, {"显示名称", values.DisplayName}, {"账号", values.Handle},
		{"主页", values.ProfileURL}, {"简介原文", values.BiographyOriginal}, {"中文简介", values.BiographyZh},
		{"简介", values.Summary}, {"备注", values.Remark}, {"说明", values.Note}, {"身份状态", values.IdentityStatus},
	} {
		if field.value != nil {
			if err := documentField(out, field.label, *field.value); err != nil {
				return err
			}
		}
	}
	if values.Accounts != nil {
		for _, account := range *values.Accounts {
			if err := documentField(out, "账号记录", strings.TrimSpace(account.Platform+" "+account.Identifier+" "+account.URL+" "+account.PublishingURL)); err != nil {
				return err
			}
		}
	}
	for _, evidence := range block.Evidence {
		if err := documentField(out, "证据（"+evidence.Status+"）", strings.TrimSpace(evidence.Title+" "+evidence.URL)); err != nil {
			return err
		}
	}
	return nil
}

// All document rows come from one immutable checkpoint. A single ordered query
// avoids a live lookup per author or work and prevents mixing two generations.
const documentRows = `WITH document(name,author_id,section,sort_key,body) AS (
 SELECT name,id,0,'',data FROM authors WHERE id!='unknown'
 UNION ALL SELECT a.name,a.id,1,p.block,p.data FROM author_profile_blocks p JOIN authors a ON a.id=p.author_id WHERE a.id!='unknown'
 UNION ALL SELECT a.name,a.id,2,m.relative_path,m.file_name FROM asset_authors x JOIN authors a ON a.id=x.author_id JOIN assets m ON m.id=x.asset_id WHERE a.id!='unknown' AND m.trashed=0
) SELECT name,author_id,section,body FROM document ORDER BY name,author_id,section,sort_key,body`

func (s *Service) BackupAuthorDocuments(ctx context.Context, p model.Principal, id string) ([]byte, Backup, error) {
	backup, err := s.Backup(ctx, p, id)
	if err != nil {
		return nil, backup, err
	}
	select {
	case authorDocumentSlots <- struct{}{}:
		defer func() { <-authorDocumentSlots }()
	case <-ctx.Done():
		return nil, backup, ctx.Err()
	}
	name := "backups/" + backup.ID + "/snapshot.sqlite"
	if err = s.safe(name); err != nil {
		return nil, backup, err
	}
	info, err := s.Root.Stat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, backup, Fail(409, "snapshot_unavailable", "该历史记录没有可读业务快照。")
	}
	if err != nil {
		return nil, backup, err
	}
	if err = s.verify(name, backup.SnapshotHash, info.Size()); err != nil {
		return nil, backup, err
	}
	absolute, err := s.Root.AbsolutePath(name)
	if err != nil {
		return nil, backup, err
	}
	db, err := store.OpenDatabase(absolute, store.SnapshotDatabase)
	if err != nil {
		return nil, backup, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, documentRows)
	if err != nil {
		return nil, backup, err
	}
	defer rows.Close()
	out := &boundedOutput{limit: 64 << 20}
	if _, err = fmt.Fprintln(out, "# 作者资料"); err != nil {
		return nil, backup, err
	}
	lastAuthor := ""
	workCount := 0
	for rows.Next() {
		var name, authorID, raw string
		var section int
		if err = rows.Scan(&name, &authorID, &section, &raw); err != nil {
			return nil, backup, err
		}
		switch section {
		case 0:
			lastAuthor = authorID
			workCount = 0
			var author model.Author
			if err = json.Unmarshal([]byte(raw), &author); err != nil {
				return nil, backup, err
			}
			_, err = fmt.Fprintf(out, "\n## %s\n\n", markdownLiteral.Replace(name))
			if err == nil {
				err = documentField(out, "别名", strings.Join(author.Aliases, " / "))
			}
			if err == nil {
				err = documentField(out, "简介", author.Biography)
			}
			if err == nil {
				err = documentField(out, "获取来源", strings.Join(author.AcquisitionSources, " / "))
			}
		case 1:
			var block AuthorProfileBlock
			if err = json.Unmarshal([]byte(raw), &block); err != nil {
				return nil, backup, err
			}
			err = documentProfile(out, block)
		case 2:
			if lastAuthor != authorID {
				return nil, backup, errors.New("author document row order changed")
			}
			workCount++
			err = documentField(out, "作品 "+strconv.Itoa(workCount), raw)
		}
		if err != nil {
			return nil, backup, Fail(422, "author_document_limit", "可读作者资料超过 64 MiB。记录备份仍包含全部资料。")
		}
	}
	if err = rows.Err(); err != nil {
		return nil, backup, err
	}
	return out.Bytes(), backup, nil
}
