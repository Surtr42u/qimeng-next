package service

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"strings"
	"time"

	"qimeng.local/next/server/internal/model"
)

// These transaction-only adapters share the normal author's indexes, list
// grammar and profile validation with the offline cold-database importer.
func InsertColdAuthor(ctx context.Context, tx *sql.Tx, author model.Author) error {
	return saveNewRegularAuthor(ctx, tx, author)
}

// Allow the original scheme, port and fragment of a public TXT citation.
// Reuse the avatar policy's host/IP checks without issuing a DNS query.
func ColdPublicHostname(host string) bool {
	if !strings.ContainsAny(host, ".:") {
		return false
	}
	probe := url.URL{Scheme: "https", Host: net.JoinHostPort(host, "443")}
	_, err := validateAvatarURL(probe.String())
	return err == nil
}

type ColdListMember = authorListBlock

func InsertColdCOSWork(ctx context.Context, tx *sql.Tx, work model.CosWork) error {
	return saveCOSPlacement(ctx, tx, nil, &work)
}

func InsertColdAuthorList(ctx context.Context, tx *sql.Tx, id, name, inputHash string, at time.Time, members []ColdListMember) error {
	list, err := nextAuthorList(ctx, tx, id, name)
	if err != nil {
		return err
	}
	for _, b := range members {
		if _, err := authorSourceTerms(b.Declaration.Sources); err != nil {
			return err
		}
	}
	list.CreatedAt, list.UpdatedAt = at, at
	list.InputHash, list.Blocks = inputHash, members
	return saveAuthorList(ctx, tx, &list)
}

// The cold store distinguishes an actual saved empty profile from an absent
// profile. Keep its explicit blank fields and its recorded verification state.
// All nonempty unverified fields use the normal candidate/archive projection.
func RestoreColdProfile(ctx context.Context, tx *sql.Tx, authorID, name, archiveID string, prepared *ImportedAuthorProfile, at time.Time) (ProfileImportResult, error) {
	result, err := RestoreImportedAuthorProfile(ctx, tx, authorID, name, archiveID, prepared)
	if err != nil || authorID == "unknown" {
		return result, err
	}
	p := prepared.profile
	if p.Status != "verified" {
		block, err := profileBlock(ctx, tx, authorID, "accounts")
		if err != nil {
			return result, err
		}
		block.Values.IdentityStatus = &p.Status
		block.Revision, block.UpdatedAt, block.Source = 1, &at, "imported"
		if err := saveProfileBlock(ctx, tx, block); err != nil {
			return result, err
		}
	}
	if p.Revision < 1 || p.Status == "candidate" {
		return result, nil
	}
	blank := func(value *string) *string {
		if value != nil && *value == "" {
			return value
		}
		return nil
	}
	for _, field := range []struct {
		block   string
		blanks  ProfileValues
		present bool
	}{
		{"basic", ProfileValues{PublicName: blank(p.Nickname), Summary: blank(p.Biography), Remark: blank(p.Remark)},
			blank(p.Nickname) != nil || blank(p.Biography) != nil || blank(p.Remark) != nil},
		{"x", ProfileValues{AvatarSourceURL: blank(p.AvatarURL)}, blank(p.AvatarURL) != nil},
	} {
		if !field.present {
			continue
		}
		block, err := profileBlock(ctx, tx, authorID, field.block)
		if err != nil {
			return result, err
		}
		if block.Revision == 0 {
			block.Values, block.Revision, block.UpdatedAt, block.Source = field.blanks, 1, &at, "imported"
			if err := saveProfileBlock(ctx, tx, block); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

func SaveColdSubscription(ctx context.Context, tx *sql.Tx, v AuthorUpdates) error {
	if v.FeedURL != "" {
		if _, err := validateAvatarURL(v.FeedURL); err != nil {
			return err
		}
	}
	if v.TrackUpdates {
		if v.FeedURL == "" || v.AuthorID == "unknown" {
			return Fail(400, "invalid_feed_subscription", "旧订阅的身份或地址无效。")
		}
		if err := requireVerifiedAuthor(ctx, tx, v.AuthorID); err != nil {
			return err
		}
	}
	for _, item := range v.Items {
		if _, err := validateAvatarURL(item.URL); err != nil {
			return err
		}
	}
	return saveAuthorUpdates(ctx, tx, v)
}

// A live v26 ID with only a positive generation floor was deleted and later
// recreated. Its profile is empty, but an earlier generation must not refill it.
func RestoreColdEmptyProfile(ctx context.Context, tx *sql.Tx, id string, floor int64, at time.Time) error {
	for _, kind := range profileBlocks {
		block := AuthorProfileBlock{AuthorID: id, Block: kind, Revision: floor, UpdatedAt: &at, Source: "imported", Evidence: []ProfileEvidence{}}
		if err := saveProfileBlock(ctx, tx, block); err != nil {
			return err
		}
	}
	return nil
}
