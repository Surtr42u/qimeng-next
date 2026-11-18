package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
	"time"
)

func testManifest() Manifest {
	hash := func(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
	return Manifest{Format: Format, Version: Version, BackupID: "backup-synthetic", InstanceID: "instance-synthetic", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Snapshot: File{Kind: "snapshot", ObjectID: "backup-synthetic", StoragePath: "qimeng.sqlite", SHA256: hash("snapshot"), Size: 8}, Files: []File{{Kind: "asset", ObjectID: "asset-synthetic", ContentVersion: 1, StoragePath: "libraries/library/image.png", SHA256: hash("original"), Size: 8}}}
}
func TestArchiveRoundTripAndCorruption(t *testing.T) {
	m := testManifest()
	var buffer bytes.Buffer
	open := func(f File) (io.ReadCloser, error) {
		text := "original"
		if f.Kind == "snapshot" {
			text = "snapshot"
		}
		return io.NopCloser(strings.NewReader(text)), nil
	}
	if e := Write(context.Background(), &buffer, m, open, nil); e != nil {
		t.Fatal(e)
	}
	read, e := Read(context.Background(), bytes.NewReader(buffer.Bytes()), nil)
	if e != nil || read.BackupID != m.BackupID || len(read.Files) != 1 {
		t.Fatalf("round trip failed: %v", e)
	}
	altered := bytes.Replace(buffer.Bytes(), []byte("original"), []byte("tampered"), 1)
	if _, e = Read(context.Background(), bytes.NewReader(altered), nil); e == nil {
		t.Fatal("corrupt payload was accepted")
	}
	trailing := append(append([]byte{}, buffer.Bytes()...), byte(1))
	if _, e = Read(context.Background(), bytes.NewReader(trailing), nil); e == nil {
		t.Fatal("unlisted trailing bytes were accepted")
	}
	if _, e = Read(context.Background(), bytes.NewReader(buffer.Bytes()[:buffer.Len()/2]), nil); e == nil {
		t.Fatal("incomplete archive was accepted")
	}
}
func TestArchiveRejectsChangedSourceAndDuplicateIdentity(t *testing.T) {
	m := testManifest()
	m.Files = append(m.Files, m.Files[0])
	if _, e := Encode(m); e == nil {
		t.Fatal("duplicate media identity accepted")
	}
	m = testManifest()
	if e := Write(context.Background(), io.Discard, m, func(f File) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("different bytes")), nil }, nil); e == nil {
		t.Fatal("changed source accepted")
	}
}
