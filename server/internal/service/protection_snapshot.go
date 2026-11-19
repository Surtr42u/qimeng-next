package service

import (
	"context"
	"database/sql"
	"errors"
	"os"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

func noProtectableInputs() error {
	return Fail(409, "no_protectable_inputs", "本批暂无可核验的成功输入，所有来源应保留。")
}

// The assets view binds each asset's current location to its content version.
// Keep the upload's original facts even when that version is no longer active.
func protectionVersions(ctx context.Context, q store.Querier, batchID string) (map[string]ProtectedInput, int, error) {
	rows, err := q.QueryContext(ctx, `SELECT i.input_id,i.ordinal,u.id,
 coalesce(json_extract(u.data,'$.assetId'),''),coalesce(json_extract(u.data,'$.publishedContentVersion'),0),
 coalesce(json_extract(u.data,'$.hash'),''),coalesce(json_extract(u.data,'$.size'),0),
 EXISTS(SELECT 1 FROM assets a WHERE a.id=json_extract(u.data,'$.assetId')
 AND a.trashed=0 AND json_extract(u.data,'$.publishedContentVersion')>0
 AND json_extract(a.data,'$.contentVersion')=json_extract(u.data,'$.publishedContentVersion')
 AND a.hash=json_extract(u.data,'$.hash') AND a.size=json_extract(u.data,'$.size'))
 FROM batch_inputs i JOIN uploads u ON u.id=i.upload_id AND u.batch_id=i.batch_id
 WHERE i.batch_id=? AND i.cancelled=0 AND u.state='ingested' LIMIT ?`, batchID, MaxBatchItems+1)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	versions := make(map[string]ProtectedInput)
	ready := 0
	for rows.Next() {
		var item ProtectedInput
		var available bool
		if err = rows.Scan(&item.InputID, &item.Ordinal, &item.UploadID, &item.AssetID, &item.ContentVersion, &item.Hash, &item.Size, &available); err != nil {
			return nil, 0, err
		}
		if available {
			item.State = "ready"
			ready++
		} else {
			item.State = "retained"
			item.Reason = "input_version_unavailable"
		}
		versions[item.InputID] = item
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(versions) > MaxBatchItems {
		return nil, 0, Fail(409, "manifest_corrupt", "原始清单数量超出批次上限。")
	}
	return versions, ready, nil
}

func snapshotBatch(ctx context.Context, db *sql.DB, batchID, fingerprint string) (model.Batch, error) {
	batch, err := store.Get[model.Batch](ctx, db, "SELECT data FROM batches WHERE id=?", batchID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return batch, err
	}
	if batch.ManifestState != "sealed" || batch.State == "cancelled" || batch.ManifestFingerprint != fingerprint {
		return batch, Fail(409, "manifest_changed", "业务快照内缺少相同的封存清单，来源应保留。")
	}
	return batch, nil
}

func snapshotProtectionItems(ctx context.Context, db *sql.DB, batch model.Batch, live map[string]ProtectedInput) ([]ProtectedInput, error) {
	frozen, err := snapshotBatch(ctx, db, batch.ID, batch.ManifestFingerprint)
	if err != nil {
		return nil, err
	}
	// A checkpoint published just before an upgrade can predate migration 0041.
	var hasRejections bool
	if err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pragma_table_info('batch_inputs') WHERE name='rejection_code')").Scan(&hasRejections); err != nil {
		return nil, err
	}
	inputs, err := readBatchInputs(ctx, db, batch.ID, hasRejections)
	if err != nil {
		return nil, err
	}
	if len(inputs) != frozen.ExpectedInputs || len(inputs) != batch.ExpectedInputs {
		return nil, Fail(409, "manifest_corrupt", "业务快照内的原始清单数量不一致。")
	}
	versions, _, err := protectionVersions(ctx, db, batch.ID)
	if err != nil {
		return nil, err
	}
	items := make([]ProtectedInput, 0, len(inputs))
	ready := 0
	for _, input := range inputs {
		item := ProtectedInput{InputID: input.ID, Ordinal: input.Ordinal, State: "retained", Reason: input.State, UploadID: input.UploadID}
		if input.State == "failed" && input.ErrorCode != "" {
			item.Reason = input.ErrorCode
		}
		if input.State == "ingested" {
			if version, ok := versions[input.ID]; ok {
				item = version
			}
			if item.State == "ready" && item == live[input.ID] {
				ready++
			} else {
				item.State = "retained"
				item.Reason = "input_version_unavailable"
			}
		}
		items = append(items, item)
	}
	if ready == 0 {
		return nil, noProtectableInputs()
	}
	return items, nil
}

// Older reports could have taken their item list from a later live database.
// The local checkpoint and the independently verified copy share this digest.
func (s *Service) legacySnapshotVersions(ctx context.Context, p BatchProtection) (map[string]ProtectedInput, error) {
	name := "backups/" + p.CheckpointID + "/snapshot.sqlite"
	if err := s.verifyContext(ctx, name, p.SnapshotHash, p.SnapshotBytes); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, Fail(409, "snapshot_unavailable", "历史检查点不可读。请取回同一摘要的快照或为当前入库版本新建保护。")
		}
		return nil, err
	}
	absolute, err := s.Root.AbsolutePath(name)
	if err != nil {
		return nil, err
	}
	db, err := store.OpenDatabase(absolute, store.SnapshotDatabase)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err = snapshotBatch(ctx, db, p.BatchID, p.ManifestFingerprint); err != nil {
		return nil, err
	}
	versions, _, err := protectionVersions(ctx, db, p.BatchID)
	return versions, err
}

func (s *Service) verifyLegacyProtectionItems(ctx context.Context, p BatchProtection, items []ProtectedInput, cache map[string]map[string]ProtectedInput) error {
	versions, ok := cache[p.ID]
	if !ok {
		var err error
		versions, err = s.legacySnapshotVersions(ctx, p)
		if err != nil {
			return err
		}
		if cache != nil {
			cache[p.ID] = versions
		}
	}
	for _, item := range items {
		if item.State == "retained" {
			continue
		}
		item.State = "ready"
		if versions[item.InputID] != item {
			return Fail(409, "input_version_unverified", "业务快照未覆盖这项输入的入库版本，来源应保留。")
		}
	}
	return nil
}
