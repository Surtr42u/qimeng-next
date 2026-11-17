package service

import (
 "encoding/json"
 "time"
)

// Profile blocks have independent revisions. Identity remains owned by Author;
// model suggestions are separate records until an owner explicitly saves them.
type ProfileAccount struct {
 Platform string `json:"platform"`
 Identifier string `json:"identifier"`
 URL string `json:"url"`
 PublishingURL string `json:"publishingUrl"`
}

type ProfileValues struct {
 Handle *string `json:"handle,omitempty"`
 ProfileURL *string `json:"profileUrl,omitempty"`
 DisplayName *string `json:"displayName,omitempty"`
 BiographyOriginal *string `json:"biographyOriginal,omitempty"`
 BiographyZh *string `json:"biographyZh,omitempty"`
 AvatarSourceURL *string `json:"avatarSourceUrl,omitempty"`
 Summary *string `json:"summary,omitempty"`
 PublicName *string `json:"publicName,omitempty"`
 Remark *string `json:"remark,omitempty"`
 Note *string `json:"note,omitempty"`
 Accounts *[]ProfileAccount `json:"accounts,omitempty"`
 IdentityStatus *string `json:"identityStatus,omitempty"`
}

type ProfileEvidence struct {
 URL string `json:"url"`
 Title string `json:"title"`
 Fields []string `json:"fields"`
 Status string `json:"status"`
 Note string `json:"note"`
 CheckedAt *time.Time `json:"checkedAt,omitempty"`
}

type ProfileAvatar struct {
 ID string `json:"id"`
 URL string `json:"url"`
 Origin string `json:"origin"`
 SourceURL string `json:"sourceUrl"`
 FetchedURL string `json:"fetchedUrl"`
 SourceHash string `json:"sourceHash"`
 ContentHash string `json:"contentHash"`
 Width int `json:"width"`
 Height int `json:"height"`
 Bytes int `json:"bytes"`
 FetchedAt time.Time `json:"fetchedAt"`
}

type AuthorProfileBlock struct {
 AuthorID string `json:"authorId"`
 Block string `json:"block"`
 Revision int64 `json:"revision"`
 Values ProfileValues `json:"values"`
 Evidence []ProfileEvidence `json:"evidence"`
 Source string `json:"source"`
 UpdatedAt *time.Time `json:"updatedAt"`
 CandidateID string `json:"candidateId,omitempty"`
 Avatar *ProfileAvatar `json:"avatar,omitempty"`
}

type AuthorProfile struct {
 AuthorID string `json:"authorId"`
 Editable bool `json:"editable"`
 Blocks []AuthorProfileBlock `json:"blocks"`
}

type AuthorProfileSummary struct {
 AuthorID string `json:"authorId"`
 XRevision int64 `json:"xRevision"`
 BiographyOriginal string `json:"biographyOriginal"`
 BiographyZh string `json:"biographyZh"`
 Avatar *ProfileAvatar `json:"avatar,omitempty"`
}

type ProfileWrite struct {
 ExpectedRevision *int64 `json:"expectedRevision"`
 Values ProfileValues `json:"values"`
 Evidence *[]ProfileEvidence `json:"evidence,omitempty"`
 CandidateID string `json:"candidateId,omitempty"`
 TranslationReviewed bool `json:"translationReviewed,omitempty"`
}

type ProfileCandidateCreate struct {
 Block string `json:"block"`
 BaseRevision *int64 `json:"baseRevision"`
 Values ProfileValues `json:"values"`
 Evidence []ProfileEvidence `json:"evidence"`
}

type ProfileCandidate struct {
 ID string `json:"id"`
 AuthorID string `json:"authorId"`
 Block string `json:"block"`
 BaseRevision int64 `json:"baseRevision"`
 Values ProfileValues `json:"values"`
 Evidence []ProfileEvidence `json:"evidence"`
 State string `json:"state"`
 CreatedAt time.Time `json:"createdAt"`
 AuthorTaskID string `json:"authorTaskId,omitempty"`
 AppliedRevision int64 `json:"appliedRevision,omitempty"`
 ArchiveID string `json:"archiveId,omitempty"`
}

type ProfileAvatarFetch struct {
 ExpectedRevision *int64 `json:"expectedRevision"`
 SourceURL string `json:"sourceUrl"`
}

type ProfileArchiveSummary struct {
 ID string `json:"id"`
 AuthorID string `json:"authorId"`
 AuthorName string `json:"authorName"`
 TargetAuthorID string `json:"targetAuthorId"`
 Reason string `json:"reason"`
 CreatedAt time.Time `json:"createdAt"`
 BlockCount int `json:"blockCount"`
 CandidateCount int `json:"candidateCount"`
 Conflicts []string `json:"conflicts"`
}

type ProfileArchive struct {
 ProfileArchiveSummary
 Blocks []AuthorProfileBlock `json:"blocks"`
 OriginalImport json.RawMessage `json:"originalImport,omitempty"`
}
