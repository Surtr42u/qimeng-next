package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStartupFailureDetailRedactsSecretsAndPrivatePaths(t *testing.T) {
	const (
		token     = "synthetic-bootstrap-token-0123456789abcdef"
		password  = "synthetic-admin-password"
		dataDir   = "/data/user/0/media.qimeng.next.synthetic/files/embedded-v1/control"
		ffmpeg    = "/data/app/media.qimeng.next.synthetic/lib/arm64/libffmpeg_cli.so"
		mediaRoot = "/storage/emulated/0/Android/data/media.qimeng.next.synthetic/files/embedded-v1/media"
	)
	storageRoots := `{"local":"` + mediaRoot + `"}`
	redactions := startupFailureRedactions(func(key string) string {
		switch key {
		case "QIMENG_DATA_DIR":
			return dataDir
		case "QIMENG_ADMIN_TOKEN":
			return token
		case "QIMENG_ADMIN_PASSWORD":
			return password
		case "QIMENG_FFMPEG":
			return ffmpeg
		case "QIMENG_STORAGE_ROOTS":
			return storageRoots
		}
		return ""
	})
	detail := "open " + dataDir + "/qimeng.sqlite: database disk image is malformed; credential=" + token +
		" password=" + password + " helper=" + ffmpeg + " media=" + mediaRoot +
		"; other /storage/emulated/0/DCIM/synthetic.jpg and /data/local/tmp/synthetic.db"
	got := sanitizeStartupFailure(detail, redactions)
	for _, leak := range []string{token, password, dataDir, ffmpeg, mediaRoot, "/storage/emulated/0/DCIM", "/data/local/tmp"} {
		if strings.Contains(got, leak) {
			t.Fatalf("failure detail leaked %q: %q", leak, got)
		}
	}
	if !strings.Contains(got, "database disk image is malformed") {
		t.Fatalf("failure detail lost the actionable reason: %q", got)
	}
	if !strings.Contains(got, "<redacted>") || !strings.Contains(got, "<path>") {
		t.Fatalf("failure detail lacks redaction markers: %q", got)
	}
}

func TestStartupFailureDetailIsSingleLineAndBounded(t *testing.T) {
	detail := strings.Repeat("open /data/user/0/synthetic/app/data.db: permission denied\r\n\t", 50)
	got := sanitizeStartupFailure(detail, nil)
	if runes := []rune(got); len(runes) > startupFailureDetailLimit {
		t.Fatalf("failure detail is not bounded: %d runes", len(runes))
	}
	if strings.ContainsAny(got, "\r\n\t") || strings.Contains(got, "  ") {
		t.Fatalf("failure detail is not one readable line: %q", got)
	}
	if strings.Contains(got, "/data/user") {
		t.Fatalf("failure detail leaked a data path: %q", got)
	}
	if empty := sanitizeStartupFailure(" \n\t ", nil); empty == "" {
		t.Fatal("failure detail disappeared instead of keeping a generic reason")
	}
}

func TestStartupFailureDocumentBindsTheLaunchAndStaysPrivate(t *testing.T) {
	dir := t.TempDir()
	receipt := filepath.Join(dir, "ready.json")
	if e := publishStartupFailure(receipt, "synthetic-launch-identity", "synthetic detail"); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(receipt + ".failed")
	if e != nil {
		t.Fatal(e)
	}
	var document startupFailureDocument
	if e = json.Unmarshal(raw, &document); e != nil {
		t.Fatal(e)
	}
	if document.LaunchID != "synthetic-launch-identity" || document.Detail != "synthetic detail" {
		t.Fatalf("failure detail did not bind the launch: %+v", document)
	}
	if _, e := os.Stat(receipt); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("failure detail replaced the startup receipt")
	}
	if runtime.GOOS != "windows" {
		info, e := os.Stat(receipt + ".failed")
		if e != nil {
			t.Fatal(e)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("failure detail is not private: %v", info.Mode().Perm())
		}
	}
	if e := publishStartupFailure("", "", "ignored"); e != nil {
		t.Fatal(e)
	}
	if entries, e := os.ReadDir(dir); e != nil || len(entries) != 1 {
		t.Fatalf("atomic rename left unexpected files: %v %v", entries, e)
	}
}
