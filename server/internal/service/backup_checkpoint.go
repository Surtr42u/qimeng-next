package service

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"

	"qimeng.local/next/server/internal/model"
)

func (s *Service) publishCheckpointDatabase(ctx context.Context, snapshot string) error {
	// VACUUM may leave an incomplete output after disk exhaustion or a crash.
	// Only a closed, synchronized file is ever published as a usable snapshot.
	staging := snapshot + ".building"
	if err := s.Root.Remove(staging); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	absolute, err := s.Root.AbsolutePath(staging)
	if err != nil { return err }
	defer s.Root.Remove(staging)
	if _, err = s.DB.DB.ExecContext(ctx, "VACUUM INTO ?", absolute); err != nil { return err }
	file, err := s.Root.OpenFile(staging, os.O_RDWR, 0600)
	if err != nil { return err }
	syncErr := file.Sync()
	closeErr := file.Close()
	if err = errors.Join(syncErr, closeErr); err != nil { return err }
	if err = s.Root.Link(staging, snapshot); err != nil { return err }
	return s.syncParent(snapshot)
}

// Match the existing checkpoint manifest encoding, including its field order
// and empty array. Large catalogs retain only the current asset and a write buffer.
func writeCheckpointManifest(ctx context.Context, db *sql.DB, out io.Writer, instanceID, snapshotHash string) (int, error) {
	header, err := json.Marshal(struct{ InstanceID, SnapshotHash string }{instanceID, snapshotHash})
	if err != nil { return 0, err }
	buffer := bufio.NewWriterSize(out, 64<<10)
	if _, err = buffer.Write(header[:len(header)-1]); err != nil { return 0, err }
	if _, err = buffer.WriteString(`,"Entries":[`); err != nil { return 0, err }
	rows, err := db.QueryContext(ctx, "SELECT data FROM assets ORDER BY id")
	if err != nil { return 0, err }
	defer rows.Close()
	count := 0
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil { return 0, err }
		var asset model.Asset
		if err = json.Unmarshal(raw, &asset); err != nil { return 0, err }
		entry := backupEntry{asset.ID,asset.ContentVersion,asset.Revision,assetPath(asset),asset.Hash,asset.Size}
		body, err := json.Marshal(entry)
		if err != nil { return 0, err }
		if count > 0 { if err = buffer.WriteByte(','); err != nil { return 0, err } }
		if _, err = buffer.Write(body); err != nil { return 0, err }
		count++
	}
	if err = rows.Err(); err != nil { return 0, err }
	if _, err = buffer.WriteString("]}"); err != nil { return 0, err }
	return count, buffer.Flush()
}

func (s *Service) publishCheckpointManifest(ctx context.Context, db *sql.DB, b Backup) (int,string,error) {
	final := "backups/" + b.ID + "/manifest.json"
	staging := final + ".building"
	file, err := s.Root.OpenFile(staging, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil { return 0,"",err }
	defer s.Root.Remove(staging)
	hash := sha256.New()
	out := io.MultiWriter(&reserveWriter{ctx:ctx,s:s,file:file},hash)
	count, err := writeCheckpointManifest(ctx,db,out,s.InstanceID,b.SnapshotHash)
	if err==nil { err=file.Sync() }
	if err=errors.Join(err,file.Close());err!=nil {return 0,"",err}
	expected:=hex.EncodeToString(hash.Sum(nil))
	if err=s.Root.Link(staging,final);errors.Is(err,os.ErrExist) {
		actual,_,readErr:=s.hashFile(final)
		if readErr!=nil {return 0,"",readErr}
		if actual!=expected {return 0,"",Fail(409,"backup_manifest_conflict","已有清单与这个业务快照不一致，请保留并核对。")}
	} else if err!=nil {return 0,"",err}
	if err=s.syncParent(final);err!=nil{return 0,"",err}
	return count,expected,nil
}
