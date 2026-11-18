package service

import (
	"bytes"
	"context"
	"image/jpeg"
	"strings"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/researchprovider"
)

type ResearchImageLookup struct {
	FrameID        string `json:"frameId"`
	FrameSHA256    string `json:"frameSha256"`
	UploadID       string `json:"uploadId,omitempty"`
	ExpectedHash   string `json:"expectedHash,omitempty"`
	AssetID        string `json:"assetId,omitempty"`
	ContentVersion int64  `json:"contentVersion,omitempty"`
}

func (i ResearchImageLookup) matches(a ResearchArtifact, frame []byte) bool {
	if a.ID != i.FrameID || a.SHA256 != i.FrameSHA256 || a.SHA256 == a.SourceSHA256 || a.MimeType != "image/jpeg" || a.UploadID != i.UploadID || a.AssetID != i.AssetID || a.ContentVersion != i.ContentVersion || i.UploadID != "" && a.SourceSHA256 != i.ExpectedHash {
		return false
	}
	dim, err := jpeg.DecodeConfig(bytes.NewReader(frame))
	return err == nil && dim.Width > 0 && dim.Width <= 640 && dim.Height > 0 && dim.Height <= 640
}

func (s *Service) LookupResearchImage(ctx context.Context, p model.Principal, key string, in ResearchImageLookup) (Result, error) {
	validSHA := func(value string) bool { return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == "" }
	intake := in.UploadID != "" && len(in.UploadID) <= 100 && validSHA(in.ExpectedHash) && in.AssetID == "" && in.ContentVersion == 0
	asset := in.AssetID != "" && len(in.AssetID) <= 100 && in.ContentVersion > 0 && in.UploadID == "" && in.ExpectedHash == ""
	if in.FrameID == "" || len(in.FrameID) > 100 || !validSHA(in.FrameSHA256) || !intake && !asset {
		return Result{}, Fail(400, "invalid_image_lookup", "请指定一张研究帧与其 SHA-256，同时明确上传及摘要或作品及内容版本。")
	}
	lookup := researchLookup{
		provider: researchprovider.Query{Name: "lens", EvidenceKind: "lead"},
		subject: PublicResearchInput{Operation: "image_lookup", UploadID: in.UploadID, ExpectedHash: in.ExpectedHash},
		image: &in,
	}
	return s.runResearchLookup(ctx, p, key, lookup, in)
}
