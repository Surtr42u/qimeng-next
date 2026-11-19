package assetmerge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"qimeng.local/next/server/internal/assetmerge/facts"
	"qimeng.local/next/server/internal/backup"
	"qimeng.local/next/server/internal/service"
)

type Options struct {
	Archive, SHA256, Stage, RestoreReceipt, Plan, PlanSHA256, Preimage, Destination string
	CatalogSHA256                                                                   string
	ReserveBytes                                                                    int64
}

type request struct {
	Stage       string                `json:"stage"`
	Destination string                `json:"destination"`
	Preimage    string                `json:"preimage"`
	ArchiveHash string                `json:"archiveSHA256"`
	PlanHash    string                `json:"planSHA256"`
	CatalogHash string                `json:"expectedCatalogSHA256,omitempty"`
	Restore     service.RestoreResult `json:"restore"`
}

type Receipt struct {
	State                 string `json:"state"`
	InstanceID            string `json:"instanceId"`
	ArchiveSHA256         string `json:"archiveSHA256"`
	BaselineSHA256        string `json:"baselineSHA256"`
	PlanSHA256            string `json:"planSHA256"`
	VerifiedAssets        int    `json:"verifiedAssets"`
	RestoredCatalogSHA256 string `json:"restoredCatalogSHA256"`
	facts.Result
}

func isDigest(s string) bool {
	value, err := hex.DecodeString(s)
	return err == nil && len(value) == sha256.Size && s == strings.ToLower(s)
}

func splitPaths(o Options) (string, request, error) {
	r := request{Stage: filepath.Base(o.Stage), Destination: filepath.Base(o.Destination), Preimage: filepath.Base(o.Preimage), ArchiveHash: o.SHA256, PlanHash: o.PlanSHA256, CatalogHash: o.CatalogSHA256}
	if !isDigest(r.ArchiveHash) || !isDigest(r.PlanHash) || (r.CatalogHash != "" && !isDigest(r.CatalogHash)) || o.ReserveBytes < 0 {
		return "", r, errors.New("expected lowercase SHA-256 fingerprints and nonnegative reserve")
	}
	parent := filepath.Dir(o.Stage)
	seen := map[string]bool{}
	for _, p := range []string{o.Stage, o.Destination, o.Preimage} {
		leaf := strings.ToLower(filepath.Base(p))
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || filepath.Dir(p) != parent || p == parent || seen[leaf] || strings.ContainsAny(leaf, "\\/:") {
			return "", r, errors.New("stage, preimage and destination must be distinct siblings in a private parent")
		}
		seen[leaf] = true
	}
	for _, p := range []string{o.Archive, o.RestoreReceipt, o.Plan} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return "", r, errors.New("input must have a clean absolute path")
		}
		for _, d := range []string{o.Stage, o.Destination, o.Preimage} {
			rel, err := filepath.Rel(d, p)
			if err == nil && (rel == "." || filepath.IsLocal(rel)) {
				return "", r, errors.New("keep the authority inputs outside the three working directories")
			}
		}
	}
	return parent, r, nil
}

func decodeStrict(b []byte, v any) error {
	if !utf8.Valid(b) {
		return errors.New("expected UTF-8 JSON")
	}
	d := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}

func externalJSON(ctx context.Context, name string, limit int64, v any) (string, error) {
	a, err := openArea(filepath.Dir(name))
	if err != nil {
		return "", err
	}
	defer a.root.Close()
	leaf := filepath.Base(name)
	info, err := a.inspect(leaf)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return "", errors.New("invalid private input file")
	}
	var b bytes.Buffer
	file, before, err := a.openRegular(leaf)
	if err != nil {
		return "", err
	}
	defer file.Close()
	n, err := io.Copy(&b, io.LimitReader(cancelReader{ctx, file}, limit+1))
	if err != nil {
		return "", err
	}
	after, err := a.inspect(leaf)
	if err != nil || n > limit || n != before.Size() || !sameInfo(before, after) {
		return "", errors.New("private input changed")
	}
	if err := decodeStrict(b.Bytes(), v); err != nil {
		return "", err
	}
	hash := sha256.Sum256(b.Bytes())
	return hex.EncodeToString(hash[:]), nil
}

func verifiedArchive(ctx context.Context, o Options, receipt service.RestoreResult) (backup.Manifest, error) {
	a, err := openArea(filepath.Dir(o.Archive))
	if err != nil {
		return backup.Manifest{}, err
	}
	defer a.root.Close()
	name := filepath.Base(o.Archive)
	file, before, err := a.openRegular(name)
	if err != nil {
		return backup.Manifest{}, err
	}
	defer file.Close()
	digest := sha256.New()
	m, err := backup.Read(ctx, io.TeeReader(file, digest), nil)
	if err != nil {
		return m, err
	}
	after, err := a.inspect(name)
	if err != nil || !sameInfo(before, after) || hex.EncodeToString(digest.Sum(nil)) != o.SHA256 {
		return m, errors.New("archive changed or checksum differs")
	}
	var assets int
	for _, f := range m.Files {
		if f.Kind == "asset" {
			assets++
		}
	}
	if receipt.State != "restored" || receipt.ArchiveHash != o.SHA256 || receipt.InstanceID == "" || receipt.InstanceID == receipt.SourceInstanceID ||
		receipt.SourceInstanceID != m.InstanceID || receipt.BackupID != m.BackupID || receipt.AssetCount != assets || receipt.UploadCount != len(m.Files)-assets {
		return m, errors.New("restore receipt does not name this complete archive")
	}
	return m, nil
}

func (a *area) writeJSON(name string, value any) error {
	if err := a.absent(name); err != nil {
		return err
	}
	f, err := a.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(f).Encode(value)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}
