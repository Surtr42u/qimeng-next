package service

import (
	"context"

	"qimeng.local/next/server/internal/model"
)

// LibraryRevisionState lets a device decide whether its preview cache still
// covers the current library. The counter deliberately tracks only changes
// which can alter thumbnails; browsing activity does not advance it.
type LibraryRevisionState struct {
	InstanceID       string `json:"instanceId"`
	Revision         int64  `json:"revision"`
	ThumbnailVersion string `json:"thumbnailVersion"`
}

func (s *Service) LibraryRevision(ctx context.Context, principal model.Principal) (LibraryRevisionState, error) {
	state := LibraryRevisionState{InstanceID: s.InstanceID, ThumbnailVersion: PreviewPolicyVersion()}
	if err := Authorize(principal, "read", ""); err != nil {
		return state, err
	}
	err := s.DB.DB.QueryRowContext(ctx, "SELECT revision FROM thumbnail_revision WHERE id=1").Scan(&state.Revision)
	return state, Normalize(err)
}
