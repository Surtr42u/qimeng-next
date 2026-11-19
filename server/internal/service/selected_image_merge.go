package service

import (
	"context"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

// Archived originals occupy native trash locations for verification and restore.
// The representative owns their active catalog and author-list relationships.
const selectedImageDonors = "(SELECT physical_asset_id FROM selected_image_merge_members WHERE physical_asset_id!=representative_id)"
const visibleTrashCount = "SELECT count(*) FROM asset_locations WHERE trashed=1 AND asset_id NOT IN " + selectedImageDonors

func selectedImageOriginal(ctx context.Context, q store.Querier, id string) (bool, error) {
	var retained bool
	err := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM selected_image_merge_members WHERE physical_asset_id=?)", id).Scan(&retained)
	return retained, err
}

func requireDiscardableOriginal(ctx context.Context, q store.Querier, id string) error {
	retained, err := selectedImageOriginal(ctx, q, id)
	if err != nil {
		return err
	}
	if retained {
		return Fail(409, "merge_original_protected", "此原件属于已校对的合并组，需要保留以供恢复。")
	}
	return nil
}

func (s *Service) selectedImage(ctx context.Context, a model.Asset) (bool, error) {
	if a.MediaType != "image" {
		return false, nil
	}
	return selectedImageOriginal(ctx, s.DB.DB, a.ID)
}
