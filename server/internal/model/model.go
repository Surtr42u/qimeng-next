// Package model defines persisted business records shared by application use cases.
package model

import "time"

type Library struct {
 ID string `json:"id"`
 Name string `json:"name"`
 Kind string `json:"kind"`
 Enabled bool `json:"enabled"`
 StorageID string `json:"storageId"`
 RootRelativePath string `json:"rootRelativePath"`
 Revision int64 `json:"revision"`
 CreatedAt time.Time `json:"createdAt"`
}

type Asset struct {
 ID string `json:"id"`
 LibraryID string `json:"libraryId"`
 FileName string `json:"fileName"`
 OriginalFileName string `json:"originalFileName"`
 SourceRelativePath string `json:"sourceRelativePath,omitempty"`
 RelativePath string `json:"relativePath"`
 MediaType string `json:"mediaType"`
 MimeType string `json:"mimeType"`
 Size int64 `json:"size"`
 Hash string `json:"hash"`
 Revision int64 `json:"revision"`
 TagsRevision int64 `json:"tagsRevision"`
 ContentVersion int64 `json:"contentVersion"`
 AuthorID string `json:"authorId"`
 AuthorIDs []string `json:"authorIds"`
 AuthorsRevision int64 `json:"authorsRevision"`
 AuthorName string `json:"authorName"`
 AuthorStatus string `json:"authorStatus"`
 CollectionID string `json:"collectionId,omitempty"`
 Page int `json:"page,omitempty"`
 Tags []string `json:"tags"`
 Favorite bool `json:"favorite"`
 Liked bool `json:"liked"`
 LikeCount int64 `json:"likeCount"`
 Note string `json:"note"`
 Source string `json:"source"`
 SourceIDs []string `json:"sourceIds"`
 AcquisitionSources []string `json:"acquisitionSources"`
 Characters []string `json:"characters"`
 CharacterIDs []string `json:"characterIds"`
 MetadataSource string `json:"metadataSource"`
 SourceMtime *time.Time `json:"sourceMtime,omitempty"`
 ContentMtime *time.Time `json:"contentMtime,omitempty"`
 CreatedAt time.Time `json:"createdAt"`
 UpdatedAt time.Time `json:"updatedAt"`
 ContentURL string `json:"contentUrl"`
 ThumbnailURL string `json:"thumbnailUrl,omitempty"`
 Width int `json:"width,omitempty"`
 Height int `json:"height,omitempty"`
 DurationMs int64 `json:"durationMs,omitempty"`
 ProbeState string `json:"probeState"`
 Trashed bool `json:"trashed"`
 StorageLayoutVersion int `json:"storageLayoutVersion,omitempty"`
 PositionMs int64 `json:"positionMs"`
 PositionContentVersion int64 `json:"positionContentVersion"`
}

type Author struct {
 ID string `json:"id"`
 Kind string `json:"kind"`
 Name string `json:"name"`
 Biography string `json:"biography"`
 Aliases []string `json:"aliases"`
 Homepage string `json:"homepage"`
 Revision int64 `json:"revision"`
 Placeholder bool `json:"placeholder"`
 Following bool `json:"following"`
 FieldStatus map[string]string `json:"fieldStatus"`
 CreatedAt time.Time `json:"createdAt"`
 UpdatedAt time.Time `json:"updatedAt"`
}

type Collection struct {
 ID string `json:"id"`
 LibraryID string `json:"libraryId"`
 Name string `json:"name"`
 Kind string `json:"kind"`
 Aliases []string `json:"aliases"`
 Revision int64 `json:"revision"`
 CreatedAt time.Time `json:"createdAt"`
}

type UploadInput struct {
 LibraryID string `json:"libraryId"`
 FileName string `json:"fileName"`
 OriginalFileName string `json:"originalFileName,omitempty"`
 SourceRelativePath string `json:"sourceRelativePath,omitempty"`
 Size int64 `json:"size"`
 MimeType string `json:"mimeType"`
 ExpectedHash string `json:"expectedHash,omitempty"`
 Mode string `json:"mode"`
 BatchID string `json:"batchId,omitempty"`
 SourceMtime *time.Time `json:"sourceMtime,omitempty"`
 RelativeDirectory string `json:"relativeDirectory,omitempty"`
 AuthorID string `json:"authorId,omitempty"`
 AuthorIDs []string `json:"authorIds,omitempty"`
 CollectionID string `json:"collectionId,omitempty"`
 Note string `json:"note,omitempty"`
 AcquisitionSources []string `json:"acquisitionSources,omitempty"`
 SourceIDs []string `json:"sourceIds,omitempty"`
 CharacterIDs []string `json:"characterIds,omitempty"`
 Family string `json:"family,omitempty"`
 ReplaceAssetID string `json:"replaceAssetId,omitempty"`
 ExpectedContentVersion int64 `json:"expectedContentVersion,omitempty"`
}

type Upload struct {
 UploadInput
 ID string `json:"id"`
 Offset int64 `json:"offset"`
 State string `json:"state"`
 AssetID string `json:"assetId,omitempty"`
 Hash string `json:"hash,omitempty"`
 ErrorCode string `json:"errorCode,omitempty"`
 ErrorMessage string `json:"errorMessage,omitempty"`
 CreatedAt time.Time `json:"createdAt"`
}
type UploadResult struct {
 Upload Upload `json:"upload"`
 Asset *Asset `json:"asset,omitempty"`
}

type Batch struct {
 ID string `json:"id"`
 LibraryID string `json:"libraryId"`
 Mode string `json:"mode"`
 Label string `json:"label"`
 MaxResearchCalls int `json:"maxResearchCalls"`
 State string `json:"state"`
 Revision int64 `json:"revision"`
 ResearchCalls int `json:"researchCalls"`
 BackupState string `json:"backupState"`
 CleanupState string `json:"cleanupState"`
 CreatedAt time.Time `json:"createdAt"`
 UpdatedAt time.Time `json:"updatedAt"`
}
type ResearchCandidate struct {
 AuthorID string `json:"authorId,omitempty"`
 Name string `json:"name,omitempty"`
 SourceURL string `json:"sourceUrl"`
 Evidence string `json:"evidence"`
}
type Research struct {
 ExpectedRevision int64 `json:"expectedRevision"`
 UploadID string `json:"uploadId"`
 ExpectedHash string `json:"expectedHash"`
 Outcome string `json:"outcome"`
 Candidates []ResearchCandidate `json:"candidates"`
 CallsUsed int `json:"callsUsed"`
}
type BatchItem struct {
 Upload Upload `json:"upload"`
 ResearchOutcome string `json:"researchOutcome"`
 Candidates []ResearchCandidate `json:"candidates"`
}
type BatchDetail struct {
 Batch Batch `json:"batch"`
 Items []BatchItem `json:"items"`
 Plans []PlanSummary `json:"plans"`
}
type PlanInput struct {
 UploadID string `json:"uploadId"`
 Family string `json:"family"`
 AuthorID string `json:"authorId,omitempty"`
 AuthorIDs []string `json:"authorIds,omitempty"`
 SourceIDs []string `json:"sourceIds,omitempty"`
 CharacterIDs []string `json:"characterIds,omitempty"`
 GroupKey string `json:"groupKey,omitempty"`
 CollectionID string `json:"collectionId,omitempty"`
 Page int `json:"page,omitempty"`
}
type PlanCreate struct {
 ExpectedRevision int64 `json:"expectedRevision"`
 Items []PlanInput `json:"items"`
}
type PlanItem struct {
 UploadID string `json:"uploadId"`
 FileName string `json:"fileName"`
 RelativePath string `json:"relativePath"`
 AuthorID string `json:"authorId"`
 AuthorIDs []string `json:"authorIds"`
 SourceIDs []string `json:"sourceIds"`
 CharacterIDs []string `json:"characterIds"`
 CollectionID string `json:"collectionId,omitempty"`
 Page int `json:"page,omitempty"`
 ExpectedHash string `json:"expectedHash"`
}
type PlanSummary struct {
 ID string `json:"id"`
 State string `json:"state"`
 Fingerprint string `json:"fingerprint"`
}
type Plan struct {
 PlanSummary
 BatchID string `json:"batchId"`
 BatchRevision int64 `json:"batchRevision"`
 DirectoryFingerprint string `json:"directoryFingerprint"`
 Items []PlanItem `json:"items"`
}

type Event struct {
 EventID string `json:"eventId"`
 AssetID string `json:"assetId"`
 FileName string `json:"fileName"`
 Kind string `json:"kind"`
 SessionID string `json:"sessionId,omitempty"`
 Seconds float64 `json:"seconds,omitempty"`
 OccurredAt *time.Time `json:"occurredAt,omitempty"`
 ContentVersion int64 `json:"contentVersion"`
 PositionMs int64 `json:"positionMs"`
 CreatedAt time.Time `json:"createdAt"`
}
type Principal struct {
 ID string `json:"id"`
 Token string `json:"token,omitempty"`
 Label string `json:"label"`
 Scopes []string `json:"scopes"`
 BatchID string `json:"batchId,omitempty"`
 AuthorTaskID string `json:"authorTaskId,omitempty"`
 ExpiresAt time.Time `json:"expiresAt"`
}
func (p Principal) Has(scope string) bool {
 for _, s := range p.Scopes { if s == "admin" || s == scope { return true } }
 return false
}
type Page[T any] struct {
 Items []T `json:"items"`
 NextCursor string `json:"nextCursor,omitempty"`
}
