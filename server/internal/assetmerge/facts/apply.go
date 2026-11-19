package facts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"qimeng.local/next/server/internal/store"
)

// Apply installs every decision in one transaction. An already committed
// decision is a receipt, including after restore or subsequent owner edits.
// Replaying it never re-applies mutable votes, favorites or catalog fields.
func Apply(ctx context.Context, db *sql.DB, plan Plan) (Result, error) {
	result, err := plan.validate()
	if err != nil {
		return Result{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback()
	var otherBaseline bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM selected_image_merge_groups
		WHERE source_id!=? OR baseline_sha256!=?)`, plan.SourceID, plan.BaselineSHA256).Scan(&otherBaseline); err != nil {
		return Result{}, err
	}
	if otherBaseline {
		return Result{}, errors.New("existing decisions belong to another source or baseline archive")
	}
	type pendingGroup struct {
		index    int
		decision string
	}
	var pending []pendingGroup
	for index, group := range plan.Groups {
		decision, err := store.Encode(group)
		if err != nil {
			return Result{}, err
		}
		var previous string
		err = tx.QueryRowContext(ctx, "SELECT decision FROM selected_image_merge_groups WHERE representative_id=?", group.RepresentativeID).Scan(&previous)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			pending = append(pending, pendingGroup{index, decision})
		case err != nil:
			return Result{}, err
		case previous != decision:
			return Result{}, fmt.Errorf("group %d conflicts with its recorded decision", index+1)
		}
	}
	if len(pending) == 0 {
		result.AlreadyApplied = true
		return result, nil
	}
	var zone string
	err = tx.QueryRowContext(ctx, `SELECT json_extract(original,'$.source_time_zone') FROM migration_relation_origins
		WHERE input_id=? AND kind='cold26/activity' AND identity='v1'`, plan.SourceID).Scan(&zone)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (zone == "" || zone == "Local")) {
		return Result{}, errors.New("missing sealed activity receipt with an explicit source time zone")
	}
	if err != nil {
		return Result{}, err
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return Result{}, errors.New("recorded source time zone is unavailable")
	}
	destinations := map[string]map[string]Group{}
	for _, item := range pending {
		g := plan.Groups[item.index]
		if destinations[g.LibraryID] == nil {
			destinations[g.LibraryID] = map[string]Group{}
		}
		destinations[g.LibraryID][strings.ToLower(g.RelativePath)] = g
	}
	for library, targets := range destinations {
		if err = checkDestinations(ctx, tx, library, targets); err != nil {
			return Result{}, err
		}
	}
	at := time.Now().UTC()
	for _, item := range pending {
		group := plan.Groups[item.index]
		project := projection{ctx: ctx, tx: tx, group: group, source: plan.SourceID, zone: location, at: at}
		if err = project.apply(plan.BaselineSHA256, item.decision); err != nil {
			return Result{}, fmt.Errorf("group %d: %w", item.index+1, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return Result{}, err
	}
	return result, nil
}

// Native archives reject case-folded storage-path collisions on every host.
// A target can take the old name of a donor in its own group. Preflight all
// groups against the same catalog so a preceding group cannot unblock another.
func checkDestinations(ctx context.Context, tx *sql.Tx, library string, targets map[string]Group) error {
	rows, err := tx.QueryContext(ctx, "SELECT asset_id,relative_path FROM asset_locations WHERE library_id=? AND trashed=0", library)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		if g, selected := targets[strings.ToLower(name)]; selected {
			member := false
			for _, m := range g.Members {
				member = member || m.AssetID == id
			}
			if !member {
				return errors.New("reviewed destination is occupied by another asset")
			}
		}
	}
	return rows.Err()
}

type projection struct {
	ctx    context.Context
	tx     *sql.Tx
	group  Group
	source string
	zone   *time.Location
	at     time.Time
}

func (p projection) exec(query string, args ...any) error {
	_, err := p.tx.ExecContext(p.ctx, query, args...)
	return err
}

const groupIDs = "(SELECT physical_asset_id FROM selected_image_merge_members WHERE representative_id=?1)"

func (p projection) apply(baseline, decision string) error {
	members, err := p.loadMembers()
	if err != nil {
		return err
	}
	rep := p.group.RepresentativeID
	if err := p.exec(`INSERT INTO selected_image_merge_groups(representative_id,source_id,baseline_sha256,decision,applied_at)
		VALUES(?,?,?,?,?)`, rep, p.source, baseline, decision, p.at.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	for _, source := range p.group.Sources {
		if err := p.exec(`INSERT INTO selected_image_merge_sources(representative_id,source_key,url,occurred_at,caption,media_url)
			VALUES(?,?,?,nullif(?,''),nullif(?,''),nullif(?,''))`, rep, source.Key, source.URL, source.At, source.Caption, source.MediaURL); err != nil {
			return err
		}
	}
	for _, member := range members {
		id := member.asset.ID
		if err := p.exec(`INSERT INTO selected_image_merge_members(physical_asset_id,representative_id,original_asset,original_relations)
			VALUES(?,?,?,?)`, id, rep, member.original, member.relations); err != nil {
			return err
		}
		if err := p.exec(`INSERT INTO selected_image_merge_aliases(source_id,source_asset_id,physical_asset_id)
			SELECT source_id,source_asset_id,asset_id FROM migration_media_aliases WHERE asset_id=?`, id); err != nil {
			return err
		}
		if err := p.exec(`INSERT INTO selected_image_merge_likes(physical_asset_id,day,created_at)
			SELECT asset_id,day,created_at FROM likes WHERE asset_id=?`, id); err != nil {
			return err
		}
		for _, key := range member.plan.SourceKeys {
			if err := p.exec("INSERT INTO selected_image_merge_source_members(representative_id,source_key,physical_asset_id) VALUES(?,?,?)", rep, key, id); err != nil {
				return err
			}
		}
	}
	if err := p.checkSupported(); err != nil {
		return err
	}
	if err := p.catalog(members); err != nil {
		return err
	}
	if err := p.activity(); err != nil {
		return err
	}
	if err := p.exec("UPDATE migration_media_aliases SET asset_id=?1 WHERE asset_id IN "+groupIDs+" AND asset_id!=?1", rep); err != nil {
		return err
	}
	if err := p.exec("UPDATE asset_locations SET trashed=1 WHERE asset_id IN "+groupIDs+" AND asset_id!=?1", rep); err != nil {
		return err
	}
	return p.exec("UPDATE asset_locations SET relative_path=?,file_name=? WHERE asset_id=?", p.group.RelativePath, path.Base(p.group.RelativePath), rep)
}

func (p projection) checkSupported() error {
	// These native relationships do not exist in the sealed image projection.
	// Their owners have their own identity, revisions or content-version rules.
	for _, table := range []string{"collection_members", "asset_playback_positions", "like_baselines", "published_plan_items", "asset_maintenance_tasks", "migration_items"} {
		var present bool
		if err := p.tx.QueryRowContext(p.ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" WHERE asset_id IN "+groupIDs+")", p.group.RepresentativeID).Scan(&present); err != nil {
			return err
		}
		if present {
			return fmt.Errorf("unsupported native relationship in %s", table)
		}
	}
	return nil
}
