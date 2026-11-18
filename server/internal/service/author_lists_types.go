package service

import (
 "time"
 "qimeng.local/next/server/internal/authorlist"
)

const maxAuthorListTextBytes=64<<20
const maxAuthorListAssets=200000
const automaticAuthorListName="上传自动挂靠.txt"

type AuthorList struct{ID string `json:"id"`;Name string `json:"name"`;Revision int64 `json:"revision"`;Position int64 `json:"position"`;CreatedAt time.Time `json:"createdAt"`;UpdatedAt time.Time `json:"updatedAt"`;AuthorCount int `json:"authorCount"`;WorkCount int `json:"workCount"`;InputHash string `json:"inputHash"`}
type authorListBlock struct{AuthorID string `json:"authorId"`;Declaration authorlist.Block `json:"declaration"`}
type authorListRecord struct{AuthorList;Blocks []authorListBlock `json:"blocks"`}
type AuthorListInput struct{Name string `json:"name"`;Content string `json:"content"`;ExpectedRevision *int64 `json:"expectedRevision"`;ConflictResolution string `json:"conflictResolution,omitempty"`}
type AuthorListMissingWork struct{AuthorID string `json:"authorId"`;AuthorName string `json:"authorName"`;AssetID string `json:"assetId"`;FileName string `json:"fileName"`}
type AuthorListResult struct{Format string `json:"format"`;List *AuthorList `json:"list,omitempty"`;AuthorsCreated int `json:"authorsCreated"`;MatchedAssets int `json:"matchedAssets"`;ChangedAssets int `json:"changedAssets"`;PreservedSources []string `json:"preservedSources"`;Issues []authorlist.Issue `json:"issues"`}
