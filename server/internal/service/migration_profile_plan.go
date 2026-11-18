package service

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"slices"

	"qimeng.local/next/server/internal/store"
)

// Each proposed block is part of the migration fingerprint and persists in
// the existing paged author report. Avatar bytes stay in the immutable input.
type V2ProfileBlockPlan struct {
	Block                string            `json:"block"`
	Action               string            `json:"action"`
	Reason               string            `json:"reason"`
	ExpectedRevision     int64             `json:"expectedRevision"`
	BeforeFingerprint    string            `json:"beforeFingerprint"`
	Fields               []string          `json:"fields"`
	CurrentValues        ProfileValues     `json:"currentValues"`
	IncomingValues       ProfileValues     `json:"incomingValues"`
	CurrentAvatarSHA256  string            `json:"currentAvatarSha256,omitempty"`
	IncomingAvatarSHA256 string            `json:"incomingAvatarSha256,omitempty"`
	IncomingEvidence     []ProfileEvidence `json:"incomingEvidence,omitempty"`
}

func planImportedProfiles(ctx context.Context, q store.Querier, plan *V2MigrationPlan, authors []V2MigrationAuthor, profiles map[string]*ImportedAuthorProfile) error {
	assigned := map[string]bool{}
	for i := range authors {
		author := &authors[i]
		profile := profiles[author.SourceAuthorID]
		if profile == nil || author.State == "conflict" {
			continue
		}
		if assigned[author.AuthorID] {
			return Fail(409, "multiple_profiles_one_author", "多个源资料映射到同一作者，请先明确各自目标。")
		}
		assigned[author.AuthorID] = true
		blocks, err := inspectImportedBlocks(ctx, q, author.AuthorID, profile)
		if err != nil {
			return err
		}
		author.ProfileBlocks = blocks
		for _, block := range blocks {
			switch block.Action {
			case "restore":
				plan.Counts["profileBlocksToRestore"]++
			case "replace":
				plan.Counts["profileBlocksToReplace"]++
			case "protected":
				plan.Counts["profileBlocksProtected"]++
			case "candidate":
				plan.Counts["profileBlocksCandidateOnly"]++
			}
		}
	}
	return nil
}

func inspectImportedBlocks(ctx context.Context, q store.Querier, authorID string, incoming *ImportedAuthorProfile) ([]V2ProfileBlockPlan, error) {
	if incoming == nil {
		return nil, nil
	}
	subscription, err := savedAuthorUpdates(ctx, q, authorID)
	if err != nil {
		return nil, err
	}
	var blocks []V2ProfileBlockPlan
	for _, source := range incoming.blocks {
		current, err := profileBlock(ctx, q, authorID, source.Block)
		if err != nil {
			return nil, err
		}
		decision, err := inspectImportedBlock(current, source, incoming, subscription)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, decision)
	}
	return blocks, nil
}

func inspectImportedBlock(current, source AuthorProfileBlock, incoming *ImportedAuthorProfile, sub AuthorUpdates) (V2ProfileBlockPlan, error) {
	d := V2ProfileBlockPlan{Block: current.Block, ExpectedRevision: current.Revision, Fields: []string{}}
	binding := struct {
		Block                AuthorProfileBlock
		SubscriptionRevision int64
		TrackUpdates         bool
		FeedURL              string
	}{Block: current}
	if current.Block == "x" || current.Block == "accounts" {
		binding.SubscriptionRevision, binding.TrackUpdates, binding.FeedURL = sub.Revision, sub.TrackUpdates, sub.FeedURL
	}
	raw, err := json.Marshal(binding)
	if err != nil {
		return d, err
	}
	d.BeforeFingerprint = digest(raw)
	if incoming.profile.Status != "verified" {
		d.Action, d.Reason = "candidate", "source_unverified"
		return d, nil
	}
	// Portable v2 does not record individual empty-field editing intentions.
	// Only confirmed, supplied material can displace imported material.
	patch := source.Values
	for _, text := range []**string{&patch.Handle, &patch.ProfileURL, &patch.BiographyOriginal, &patch.BiographyZh, &patch.AvatarSourceURL, &patch.PublicName, &patch.Summary, &patch.Remark} {
		if *text != nil && **text == "" {
			*text = nil
		}
	}
	if patch.Accounts != nil && len(*patch.Accounts) == 0 {
		patch.Accounts = nil
	}
	d.CurrentValues = current.Values
	d.IncomingValues = mergeProfileValues(current.Values, patch)
	if current.Avatar != nil {
		d.CurrentAvatarSHA256 = current.Avatar.ContentHash
	}
	d.IncomingAvatarSHA256 = d.CurrentAvatarSHA256
	if source.Block == "x" {
		accountChanged := patch.ProfileURL != nil && canonicalXProfile(*patch.ProfileURL) != canonicalXProfile(profileString(current.Values.ProfileURL))
		originalChanged := patch.BiographyOriginal != nil && *patch.BiographyOriginal != profileString(current.Values.BiographyOriginal)
		if accountChanged && patch.BiographyOriginal == nil {
			d.IncomingValues.BiographyOriginal = nil
		}
		if (accountChanged || originalChanged) && patch.BiographyZh == nil {
			d.IncomingValues.BiographyZh = nil
		}
		if accountChanged && patch.AvatarSourceURL == nil {
			d.IncomingValues.AvatarSourceURL = nil
		}
		if accountChanged || profileString(d.IncomingValues.AvatarSourceURL) != profileString(current.Values.AvatarSourceURL) {
			d.IncomingAvatarSHA256 = ""
		}
		if incoming.avatar != nil {
			d.IncomingAvatarSHA256 = digest(incoming.avatar.body)
		}
	}
	before, err := profileFieldBytes(d.CurrentValues)
	if err != nil {
		return d, err
	}
	after, err := profileFieldBytes(d.IncomingValues)
	if err != nil {
		return d, err
	}
	covered := profileFields(patch)
	for field := range covered {
		if !bytes.Equal(before[field], after[field]) {
			d.Fields = append(d.Fields, field)
			covered[field] = true
		}
	}
	avatarBindingChanged := source.Block == "x" && incoming.avatar != nil && current.Avatar != nil &&
		(current.Avatar.SourceURL != profileString(patch.AvatarSourceURL) || current.Avatar.SourceHash != incoming.avatar.sourceHash)
	if d.IncomingAvatarSHA256 != d.CurrentAvatarSHA256 || avatarBindingChanged {
		d.Fields = append(d.Fields, "avatar")
	}
	d.IncomingEvidence = importReplacementEvidence(current.Evidence, source.Evidence, covered, d.Fields)
	if !slices.EqualFunc(current.Evidence, d.IncomingEvidence, func(a, b ProfileEvidence) bool { return reflect.DeepEqual(a, b) }) {
		d.Fields = append(d.Fields, "evidence")
	}
	slices.Sort(d.Fields)
	switch {
	case len(d.Fields) == 0:
		d.Action, d.Reason = "keep", "unchanged"
	case current.AuthorID == "unknown":
		d.Action, d.Reason = "protected", "placeholder"
	case current.Revision > 0 && current.Source != "imported":
		d.Action, d.Reason = "protected", "local_profile_protected"
	case binding.TrackUpdates:
		d.Action, d.Reason = "protected", "active_subscription"
	case current.Revision == 0:
		d.Action, d.Reason = "restore", "missing_block"
	case len(d.IncomingEvidence) > 20:
		d.Action, d.Reason = "protected", "evidence_capacity"
	default:
		d.Action, d.Reason = "replace", "verified_import"
	}
	return d, nil
}

func profileFieldBytes(values ProfileValues) (map[string]json.RawMessage, error) {
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	err = json.Unmarshal(raw, &fields)
	return fields, err
}

func importReplacementEvidence(current, incoming []ProfileEvidence, covered map[string]bool, changed []string) []ProfileEvidence {
	result := []ProfileEvidence{}
	for _, e := range current {
		e.Fields = slices.DeleteFunc(slices.Clone(e.Fields), func(field string) bool { return slices.Contains(changed, field) })
		if len(e.Fields) > 0 {
			result = append(result, e)
		}
	}
	for _, e := range incoming {
		e.Fields = slices.DeleteFunc(slices.Clone(e.Fields), func(field string) bool { return !covered[field] })
		if len(e.Fields) == 0 {
			continue
		}
		index := slices.IndexFunc(result, func(prior ProfileEvidence) bool {
			return prior.URL == e.URL && prior.Title == e.Title && prior.Note == e.Note && prior.Status == e.Status && reflect.DeepEqual(prior.CheckedAt, e.CheckedAt)
		})
		if index < 0 {
			result = append(result, e)
		} else {
			result[index].Fields = appendUnique(result[index].Fields, e.Fields...)
			slices.Sort(result[index].Fields)
		}
	}
	return result
}
