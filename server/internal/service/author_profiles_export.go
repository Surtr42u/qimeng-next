package service

import (
	"context"

	"qimeng.local/next/server/internal/store"
)

// Stored text is not necessarily a verified identity. Export only confirmed
// account associations; never mine arbitrary notes, avatar URLs, or candidates.
func confirmedAuthorSourceLinks(ctx context.Context, q store.Querier, authorID string) ([]string, error) {
	x, e := profileBlock(ctx, q, authorID, "x")
	if e != nil {
		return nil, e
	}
	basic, e := profileBlock(ctx, q, authorID, "basic")
	if e != nil {
		return nil, e
	}
	accounts, e := profileBlock(ctx, q, authorID, "accounts")
	if e != nil {
		return nil, e
	}
	return confirmedProfileLinks(x, basic, accounts), nil
}
func confirmedProfileLinks(x, basic, accounts AuthorProfileBlock) []string {
	links := []string{}
	verified := profileString(accounts.Values.IdentityStatus) == "verified"
	xURL := profileString(x.Values.ProfileURL)
	xConfirmed := verified
	if xURL != "" {
		for _, evidence := range x.Evidence {
			if evidence.Status == "confirmed" && evidence.URL == xURL {
				xConfirmed = true
			}
		}
	}
	if xConfirmed && xURL != "" {
		links = appendUnique(links, xURL)
	}
	if verified && accounts.Values.Accounts != nil {
		for _, account := range *accounts.Values.Accounts {
			if account.URL != "" {
				links = appendUnique(links, account.URL)
			}
			if account.PublishingURL != "" {
				links = appendUnique(links, account.PublishingURL)
			}
		}
	}
	for _, block := range []AuthorProfileBlock{x, basic, accounts} {
		for _, evidence := range block.Evidence {
			if evidence.Status == "confirmed" && evidence.URL != "" {
				links = appendUnique(links, evidence.URL)
			}
		}
	}
	return links
}
