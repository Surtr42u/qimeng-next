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
 FileCount int64 `json:"fileCount"`
 ScanState string `json:"scanState"`
 LatestScanID string `json:"latestScanId,omitempty"`
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
 TagSetUpdatedAt *time.Time `json:"tagSetUpdatedAt,omitempty"`
 TagSetSource string `json:"tagSetSource,omitempty"`
 ContentVersion int64 `json:"contentVersion"`
 AuthorID string `json:"authorId"`
 AuthorIDs []string `json:"authorIds"`
 AuthorsRevision int64 `json:"authorsRevision"`
 AuthorName string `json:"authorName"`
 AuthorStatus string `json:"authorStatus"`
 CollectionID string `json:"collectionId,omitempty"`
 CosWorkID string `json:"cosWorkId,omitempty"`
 COSDirectoryAuthorID string `json:"cosDirectoryAuthorId,omitempty"`
 Page int `json:"page,omitempty"`
 Tags []string `json:"tags"`
 Favorite bool `json:"favorite"`
 FavoriteUpdatedAt *time.Time `json:"favoriteUpdatedAt,omitempty"`
 FavoritedAt *time.Time `json:"favoritedAt,omitempty"`
 Liked bool `json:"liked"`
 LikeCount int64 `json:"likeCount"`
 UnattributedLikeCount int64 `json:"unattributedLikeCount"`
 ViewCount int64 `json:"viewCount"`
 PlayCount int64 `json:"playCount"`
 CosWorkName string `json:"cosWorkName,omitempty"`
 WorkKeys []string `json:"workKeys,omitempty"`
 RoleKeys []string `json:"roleKeys,omitempty"`
 WorkFacets []FacetReference `json:"workFacets,omitempty"`
 RoleFacets []FacetReference `json:"roleFacets,omitempty"`
 Note string `json:"note"`
 Source string `json:"source"`
 SourceIDs []string `json:"sourceIds"`
 AcquisitionSources []string `json:"acquisitionSources"`
 Characters []string `json:"characters"`
 CharacterIDs []string `json:"characterIds"`
 MetadataSource string `json:"metadataSource"`
 SourceMtime *time.Time `json:"sourceMtime,omitempty"`
 ContentMtime *time.Time `json:"contentMtime,omitempty"`
 FileDate *time.Time `json:"fileDate,omitempty"`
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

type FacetReference struct{Key string `json:"key"`;Kind string `json:"kind"`;ID string `json:"id"`;Name string `json:"name"`}

func(a Asset)EffectiveFileDate()*time.Time{if a.ContentMtime!=nil{return a.ContentMtime};return a.SourceMtime}

type Author struct {
 ID string `json:"id"`
 Kind string `json:"kind"`
 Name string `json:"name"`
 Biography string `json:"biography"`
 Aliases []string `json:"aliases"`
 Homepage string `json:"homepage"`
 AcquisitionSources []string `json:"acquisitionSources"`
 Revision int64 `json:"revision"`
 Placeholder bool `json:"placeholder"`
 Following bool `json:"following"`
 FollowingUpdatedAt *time.Time `json:"followingUpdatedAt,omitempty"`
 FileCount int64 `json:"fileCount"`
 ViewCount int64 `json:"viewCount"`
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

type CosWork struct {ID string `json:"id"`;LibraryID string `json:"libraryId"`;AuthorID string `json:"authorId"`;RelativeDirectory string `json:"relativeDirectory"`;Name string `json:"name"`;Revision int64 `json:"revision"`;CreatedAt time.Time `json:"createdAt"`}

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
 InputID string `json:"inputId,omitempty"`
 SourceMtime *time.Time `json:"sourceMtime,omitempty"`
 RelativeDirectory string `json:"relativeDirectory,omitempty"`
 AuthorID string `json:"authorId,omitempty"`
 AuthorIDs []string `json:"authorIds,omitempty"`
 CollectionID string `json:"collectionId,omitempty"`
 CosWorkID string `json:"cosWorkId,omitempty"`
 Note string `json:"note,omitempty"`
 AcquisitionSources []string `json:"acquisitionSources,omitempty"`
 AuthorAcquisitionSources []string `json:"authorAcquisitionSources,omitempty"`
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
 PublishedContentVersion int64 `json:"publishedContentVersion,omitempty"`
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
 ManifestRevision int64 `json:"manifestRevision"`
 ManifestState string `json:"manifestState"`
 ManifestFingerprint string `json:"manifestFingerprint"`
 ExpectedInputs int `json:"expectedInputs"`
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
 Inputs []BatchInput `json:"inputs"`
 Items []BatchItem `json:"items"`
 Plans []PlanSummary `json:"plans"`
}
type IntakeInput struct {
 ID string `json:"id"`
 NameState string `json:"nameState"`
 OriginalFileName string `json:"originalFileName,omitempty"`
 SourceRelativePath string `json:"sourceRelativePath,omitempty"`
 Size *int64 `json:"size,omitempty"`
 ExpectedHash string `json:"expectedHash,omitempty"`
}
type BatchInput struct {
 IntakeInput
 Ordinal int `json:"ordinal"`
 State string `json:"state"`
 UploadID string `json:"uploadId,omitempty"`
 AssetID string `json:"assetId,omitempty"`
 ErrorCode string `json:"errorCode,omitempty"`
}
type BatchInputsSet struct {
 ExpectedManifestRevision int64 `json:"expectedManifestRevision"`
 Inputs []IntakeInput `json:"inputs"`
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
 Asset *Asset `json:"asset,omitempty"`
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
 ParentDelegationID string `json:"parentDelegationId,omitempty"`
 Generation int64 `json:"generation,omitempty"`
 ExpiresAt time.Time `json:"expiresAt"`
}
func (p Principal) Has(scope string) bool {
 for _, s := range p.Scopes { if s == "admin" || s == scope { return true } }
 return false
}
type DateCountBucket struct {Date string `json:"date"`;FileCount int64 `json:"fileCount"`}
type Page[T any] struct {
 Items []T `json:"items"`
 NextCursor string `json:"nextCursor,omitempty"`
 TotalMatched *int64 `json:"totalMatched,omitempty"`
 TotalOptions *int64 `json:"totalOptions,omitempty"`
 DateCounts *[]DateCountBucket `json:"dateCounts,omitempty"`
 UnknownDateCount *int64 `json:"unknownDateCount,omitempty"`
}
