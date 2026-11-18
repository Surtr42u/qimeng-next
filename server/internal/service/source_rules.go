package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/sourcerules"
	"qimeng.local/next/server/internal/store"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

type SourceRuleUpdate struct {
	sourcerules.Layer
	ExpectedRevision int64 `json:"expectedRevision"`
}
type SourceRuleSettings struct {
	sourcerules.Layer
	Revision     int64  `json:"revision"`
	RebuildState string `json:"rebuildState"`
	Processed    int64  `json:"processed"`
	Matched      int64  `json:"matched"`
	Ambiguous    int64  `json:"ambiguous"`
	ErrorCode    string `json:"errorCode,omitempty"`
}
type sourceRuleSnapshot struct {
	revision int64
	index    *sourcerules.Index
}
type sourceRuleWorker struct {
	snapshot atomic.Pointer[sourceRuleSnapshot]
	wake     chan struct{}
}

func readSourceRules(ctx context.Context, q store.Querier) (SourceRuleSettings, error) {
	var result SourceRuleSettings
	var raw []byte
	err := q.QueryRowContext(ctx, "SELECT revision,layer,run_state,processed,matched,ambiguous,error_code FROM source_rule_settings WHERE id=1").Scan(&result.Revision, &raw, &result.RebuildState, &result.Processed, &result.Matched, &result.Ambiguous, &result.ErrorCode)
	if err == nil {
		err = json.Unmarshal(raw, &result.Layer)
	}
	return result, err
}
func (s *Service) SourceRules(ctx context.Context) (SourceRuleSettings, error) {
	return readSourceRules(ctx, s.DB.DB)
}
func (s *Service) SetSourceRules(ctx context.Context, w *Write, in SourceRuleUpdate) (SourceRuleSettings, error) {
	var out SourceRuleSettings
	if !w.Principal.Has("admin") {
		return out, Fail(403, "scope_denied", "词表写入需要本人权限。")
	}
	layer, err := sourcerules.Normalize(in.Layer)
	if err != nil {
		return out, Fail(400, "invalid_source_rules", "请核对词组、角色、别名与停用词的名称和数量。")
	}
	q, err := w.Q()
	if err != nil {
		return out, err
	}
	state, err := readSourceRules(ctx, q)
	if err != nil {
		return out, err
	}
	if state.Revision != in.ExpectedRevision {
		return out, Conflict(state.Revision)
	}
	raw, err := json.Marshal(layer)
	if err != nil {
		return out, err
	}
	prior, err := json.Marshal(state.Layer)
	if err != nil {
		return out, err
	}
	if string(prior) == string(raw) && state.RebuildState != "failed" {
		return state, nil
	}
	out = SourceRuleSettings{Layer: layer, Revision: state.Revision + 1, RebuildState: "pending"}
	_, err = q.ExecContext(ctx, "UPDATE source_rule_settings SET layer=?,revision=?,engine_version=1,run_state='pending',cursor='',processed=0,matched=0,ambiguous=0,error_code='' WHERE id=1", string(raw), out.Revision)
	if err != nil {
		return out, err
	}
	w.after = append(w.after, func() error { s.wakeSourceRules(); return nil })
	return out, nil
}
func (s *Service) ruleIndex(ctx context.Context, q store.Querier) (*sourcerules.Index, error) {
	var revision int64
	if err := q.QueryRowContext(ctx, "SELECT revision FROM source_rule_settings WHERE id=1").Scan(&revision); err != nil {
		return nil, err
	}
	if cached := s.sourceRules.snapshot.Load(); cached != nil && cached.revision == revision {
		return cached.index, nil
	}
	settings, err := readSourceRules(ctx, q)
	if err != nil {
		return nil, err
	}
	snap := &sourceRuleSnapshot{settings.Revision, sourcerules.Build(settings.Layer)}
	s.sourceRules.snapshot.Store(snap)
	return snap.index, nil
}

// Apply only to a previously rule-produced taxonomy, or a genuinely unassigned one.
// Existing/manual evidence, including an owner's deliberate empty field, has priority.
func (s *Service) applySourceRules(ctx context.Context, q store.Querier, a *model.Asset) (changed bool, err error) {
	if a.Trashed || !a.TaxonomyDerived && (a.Source != "" || len(a.SourceIDs) > 0 || len(a.CharacterIDs) > 0 || len(a.Characters) > 0 || a.MetadataSource == "migration") {
		return false, nil
	}
	var unavailable bool
	err = q.QueryRowContext(ctx, `SELECT (SELECT kind FROM libraries WHERE id=?)!='regular' OR EXISTS(SELECT 1 FROM asset_human_fields WHERE asset_id=? AND field='taxonomy')`, a.LibraryID, a.ID).Scan(&unavailable)
	if err != nil || unavailable {
		return false, err
	}
	index, err := s.ruleIndex(ctx, q)
	if err != nil {
		return false, err
	}
	result := index.Match(a.FileName)
	roleCount := 0
	for _, match := range result.Matches {
		roleCount += len(match.Characters)
	}
	if len(result.Matches) > 20 || roleCount > 50 {
		for _, match := range result.Matches {
			result.Candidates = appendUnique(result.Candidates, match.Source)
			for _, name := range match.Characters {
				result.Candidates = appendUnique(result.Candidates, match.Source+" / "+name)
			}
		}
		result.Matches = nil
	}
	ids, roles, names, characters := []string{}, []string{}, []string{}, []string{}
	roleNames := map[string]string{}
	for _, m := range result.Matches {
		source, uncertain, e := s.ruleTerm(ctx, q, "", m.Source)
		if e != nil {
			return false, e
		}
		if len(uncertain) > 0 {
			result.Candidates = appendUnique(result.Candidates, uncertain...)
			continue
		}
		ids = appendUnique(ids, source)
		names = appendUnique(names, m.Source)
		for _, name := range m.Characters {
			role, ambiguous, e := s.ruleTerm(ctx, q, source, name)
			if e != nil {
				return false, e
			}
			if len(ambiguous) > 0 {
				for _, entry := range ambiguous {
					result.Candidates = appendUnique(result.Candidates, m.Source+" / "+entry)
				}
				continue
			}
			roles = appendUnique(roles, role)
			roleNames[role] = name
			characters = appendUnique(characters, name)
		}
	}
	if len(result.Candidates) > 0 {
		ids, roles, names, characters = []string{}, []string{}, []string{}, []string{}
		slices.Sort(result.Candidates)
	}
	derived := len(ids) > 0 || len(result.Candidates) > 0
	source := strings.Join(names, "+")
	slices.Sort(characters)
	slices.SortStableFunc(roles, func(a, b string) int { return strings.Compare(roleNames[a], roleNames[b]) })
	if derived == a.TaxonomyDerived && source == a.Source && slices.Equal(a.SourceIDs, ids) && slices.Equal(a.CharacterIDs, roles) && slices.Equal(a.Characters, characters) && slices.Equal(a.TaxonomyCandidates, result.Candidates) {
		return false, nil
	}
	a.Source, a.SourceIDs, a.CharacterIDs, a.Characters = source, ids, roles, characters
	a.TaxonomyDerived, a.TaxonomyCandidates = derived, result.Candidates
	return true, nil
}
func (s *Service) ruleTerm(ctx context.Context, q store.Querier, source, name string) (string, []string, error) {
	kind := "source"
	if source != "" {
		kind = "character"
	}
	deterministic := digest([]byte(s.InstanceID + "\x00rule-" + kind + "\x00" + source + "\x00" + name))[:32]
	var rows *sql.Rows
	var err error
	if source == "" {
		rows, err = q.QueryContext(ctx, "SELECT id,name,'' FROM sources WHERE name=? OR id=? ORDER BY id LIMIT 101", name, deterministic)
	} else {
		rows, err = q.QueryContext(ctx, "SELECT id,name,source_id FROM characters WHERE (source_id=? AND name=?) OR id=? ORDER BY id LIMIT 101", source, name, deterministic)
	}
	if err != nil {
		return "", nil, err
	}
	var candidates []string
	var first string
	var stale bool
	for rows.Next() {
		var id, canonical, owner string
		if err = rows.Scan(&id, &canonical, &owner); err != nil {
			rows.Close()
			return "", nil, err
		}
		first = id
		candidates = append(candidates, canonical+"（"+id+"）")
		if canonical != name || owner != source {
			stale = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", nil, err
	}
	if len(candidates) > 100 {
		candidates[100] = "更多同名身份须在词表中核对"
	}
	if len(candidates) > 1 || stale {
		return "", candidates, nil
	}
	if len(candidates) == 1 {
		return first, nil, nil
	}
	now := time.Now().UTC()
	term := Source{ID: deterministic, Name: name, Aliases: []string{}, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if source == "" {
		raw, e := store.Encode(term)
		if e != nil {
			return "", nil, e
		}
		_, err = q.ExecContext(ctx, "INSERT INTO sources(id,name,data) VALUES(?,?,?)", term.ID, term.Name, raw)
	} else {
		raw, e := store.Encode(Character{Source: term, SourceID: source})
		if e != nil {
			return "", nil, e
		}
		_, err = q.ExecContext(ctx, "INSERT INTO characters(id,source_id,name,data) VALUES(?,?,?,?)", term.ID, source, term.Name, raw)
	}
	return deterministic, nil, err
}
func (s *Service) wakeSourceRules() {
	if s.sourceRules.wake != nil {
		select {
		case s.sourceRules.wake <- struct{}{}:
		default:
		}
	}
}
func (s *Service) startSourceRules(ctx context.Context) error {
	if _, err := s.DB.DB.ExecContext(ctx, `UPDATE source_rule_settings SET engine_version=1,run_state='pending',cursor='',processed=0,matched=0,ambiguous=0,error_code='' WHERE engine_version!=1`); err != nil {
		return err
	}
	s.sourceRules.wake = make(chan struct{}, 1)
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		for {
			select {
			case <-s.workCtx.Done():
				return
			case <-s.sourceRules.wake:
			}
			for {
				if s.workCtx.Err() != nil {
					return
				}
				again := s.runSourceRulePage(s.workCtx)
				if !again {
					break
				}
			}
		}
	}()
	s.wakeSourceRules()
	return nil
}
func (s *Service) runSourceRulePage(ctx context.Context) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.DB.DB.BeginTx(ctx, nil)
	if err != nil {
		s.recordRuleRebuildError(ctx, err)
		return false
	}
	more, err := s.sourceRulePage(ctx, tx)
	if err == nil {
		err = tx.Commit()
	} else {
		tx.Rollback()
	}
	if err != nil {
		s.recordRuleRebuildError(ctx, err)
	}
	return err == nil && more
}
func (s *Service) recordRuleRebuildError(ctx context.Context, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	// The caller holds the business write lock, so this cannot mark a replacement rule revision failed.
	if _, saveErr := s.DB.DB.ExecContext(ctx, "UPDATE source_rule_settings SET run_state='failed',error_code='rebuild_unavailable' WHERE id=1"); saveErr != nil {
		slog.Error("unable to save source vocabulary reconciliation failure")
	}
}
func (s *Service) sourceRulePage(ctx context.Context, tx *sql.Tx) (bool, error) {
	var state, cursor string
	if err := tx.QueryRowContext(ctx, "SELECT run_state,cursor FROM source_rule_settings WHERE id=1").Scan(&state, &cursor); err != nil {
		return false, err
	}
	if state != "pending" && state != "running" {
		return false, nil
	}
	const size = 24
	items, err := store.List[model.Asset](ctx, tx, `SELECT a.data FROM assets a JOIN libraries l ON a.library_id=l.id WHERE a.id>? AND l.kind='regular' AND a.trashed=0 AND NOT EXISTS(SELECT 1 FROM file_intents f WHERE f.state='prepared' AND json_extract(f.data,'$.After.id')=a.id) ORDER BY a.id LIMIT ?`, cursor, size)
	if err != nil {
		return false, err
	}
	matched, ambiguous := 0, 0
	for _, a := range items {
		changed, e := s.applySourceRules(ctx, tx, &a)
		if e != nil {
			return false, e
		}
		if changed {
			a.Revision++
			a.UpdatedAt = time.Now().UTC()
			if e = store.SaveAssetEdits(ctx, tx, a, false, store.CompleteTaxonomy(a)); e != nil {
				return false, e
			}
		}
		if a.TaxonomyDerived && len(a.SourceIDs) > 0 {
			matched++
		}
		if len(a.TaxonomyCandidates) > 0 {
			ambiguous++
		}
		cursor = a.ID
	}
	state = "completed"
	if len(items) == size {
		state = "running"
	}
	_, err = tx.ExecContext(ctx, "UPDATE source_rule_settings SET run_state=?,cursor=?,processed=processed+?,matched=matched+?,ambiguous=ambiguous+? WHERE id=1", state, cursor, len(items), matched, ambiguous)
	return state == "running", err
}
