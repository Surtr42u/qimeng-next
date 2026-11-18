package service

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"time"

	"qimeng.local/next/server/internal/model"
	"qimeng.local/next/server/internal/store"
)

// This is an explicit mapping of the documented v2 CreatorProfile payload,
// independently implemented here. Unknown fields stay in the raw archive.
type importedCreatorProfile struct {
	AuthorID     string   `json:"authorId"`
	Revision     int64    `json:"revision"`
	Nickname     *string  `json:"nickname"`
	Biography    *string  `json:"biography"`
	Remark       *string  `json:"remark"`
	AvatarURL    *string  `json:"avatarUrl"`
	HomepageURL  *string  `json:"homepageUrl"`
	FeedURL      *string  `json:"feedUrl"`
	EvidenceURLs []string `json:"evidenceUrls"`
	Status       string   `json:"status"`
	XProfile     *struct {
		URL         string  `json:"url"`
		Biography   string  `json:"biography"`
		BiographyZh *string `json:"biographyZh"`
	} `json:"xProfile"`
	Candidate *importedCreatorProfile `json:"candidate"`
}

type ImportedAuthorProfile struct {
	original json.RawMessage
	profile  importedCreatorProfile
	blocks   []AuthorProfileBlock
	avatar   *normalizedProfileAvatar
}

func (p *ImportedAuthorProfile) AvatarStorageBytes() int64 {
	if p == nil || p.avatar == nil {
		return 0
	}
	return int64(len(p.avatar.body))
}

type ProfileImportResult struct {
	RestoredBlocks  []string `json:"restoredBlocks"`
	CandidateBlocks []string `json:"candidateBlocks"`
	AvatarRestored  bool     `json:"avatarRestored"`
	Issues          []string `json:"issues"`
}

// Prepare is called during migration's input-validation phase, before any
// business write. It never makes a network request or enables old refresh jobs.
func PrepareImportedAuthorProfile(ctx context.Context, sourceAuthorID string, raw json.RawMessage, avatar []byte) (*ImportedAuthorProfile, error) {
	if len(raw) == 0 || len(raw) > 256<<10 {
		return nil, Fail(400, "invalid_imported_profile", "迁入作者资料必须为不超过 256 KiB 的 JSON 对象。")
	}
	prepared := &ImportedAuthorProfile{}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil || json.Unmarshal(raw, &prepared.profile) != nil {
		return nil, Fail(400, "invalid_imported_profile", "迁入作者资料格式无效。")
	}
	prepared.original = append(json.RawMessage(nil), raw...)
	if prepared.profile.AuthorID != sourceAuthorID {
		return nil, Fail(400, "imported_profile_identity", "资料与迁入作者身份不一致。")
	}
	if prepared.profile.Status != "empty" && prepared.profile.Status != "candidate" && prepared.profile.Status != "verified" {
		return nil, Fail(400, "invalid_imported_profile", "迁入资料核实状态无效。")
	}
	var err error
	prepared.blocks, err = convertImportedProfile(prepared.profile)
	if err != nil {
		return nil, err
	}
	if prepared.profile.Candidate != nil {
		if prepared.profile.Candidate.Candidate != nil {
			return nil, Fail(400, "invalid_imported_profile", "迁入候选不能嵌套其他候选。")
		}
		if _, err = convertImportedProfile(*prepared.profile.Candidate); err != nil {
			return nil, err
		}
	}
	if len(avatar) > 0 {
		if len(avatar) > 2<<20 || profileString(prepared.profile.AvatarURL) == "" {
			return nil, Fail(400, "imported_avatar_binding", "迁入头像最多 2 MiB，且必须绑定同条资料中的头像来源。")
		}
		release, err := withAvatarSlot(ctx)
		if err != nil {
			return nil, err
		}
		normalized, err := normalizeProfileAvatar(avatar)
		release()
		if err != nil {
			return nil, err
		}
		prepared.avatar = &normalized
	}
	return prepared, nil
}

func convertImportedProfile(in importedCreatorProfile) ([]AuthorProfileBlock, error) {
	for _, item := range []struct {
		value *string
		limit int
	}{
		{in.Nickname, 200}, {in.Biography, 2000}, {in.Remark, 500}, {in.AvatarURL, 2000}, {in.HomepageURL, 2000}, {in.FeedURL, 2000},
	} {
		if item.value != nil && !profileText(*item.value, item.limit) {
			return nil, Fail(400, "invalid_imported_profile", "迁入资料字段超过文档规定的长度。")
		}
	}
	if len(in.EvidenceURLs) > 8 {
		return nil, Fail(400, "invalid_imported_profile", "迁入资料来源证据超过上限。")
	}
	for _, link := range append(append([]string{}, in.EvidenceURLs...), profileString(in.HomepageURL), profileString(in.FeedURL)) {
		if link != "" && (!profileLink(link) || !strings.HasPrefix(link, "https://") || !profileText(link, 2000)) {
			return nil, Fail(400, "invalid_imported_profile", "迁入档案网址须为有效 HTTPS 公开链接。")
		}
	}
	basic := ProfileValues{PublicName: in.Nickname, Summary: in.Biography, Remark: in.Remark}
	x := ProfileValues{AvatarSourceURL: in.AvatarURL}
	accounts := []ProfileAccount{}
	if homepage := profileString(in.HomepageURL); homepage != "" || profileString(in.FeedURL) != "" {
		platform := "公开发布"
		if u, err := url.Parse(homepage); err == nil && u.Hostname() != "" {
			platform = u.Hostname()
		}
		accounts = append(accounts, ProfileAccount{Platform: platform, URL: homepage, PublishingURL: profileString(in.FeedURL)})
	}
	if in.XProfile != nil {
		if !profileText(in.XProfile.URL, 2000) || !profileText(in.XProfile.Biography, 1000) || (in.XProfile.BiographyZh != nil && !profileText(*in.XProfile.BiographyZh, 2000)) {
			return nil, Fail(400, "invalid_imported_x_profile", "迁入 X 资料超过长度限制。")
		}
		if in.XProfile.URL != "" && canonicalXProfile(in.XProfile.URL) == "" {
			return nil, Fail(400, "invalid_imported_x_profile", "迁入 X 账号主页无效。")
		}
		if in.XProfile.Biography == "" && profileString(in.XProfile.BiographyZh) != "" {
			return nil, Fail(400, "invalid_imported_x_profile", "迁入译文缺少对应原文。")
		}
		x.ProfileURL = &in.XProfile.URL
		x.BiographyOriginal = &in.XProfile.Biography
		x.BiographyZh = in.XProfile.BiographyZh
		if handle := canonicalXProfile(in.XProfile.URL); handle != "" {
			x.Handle = &handle
		}
	}
	accountValues := ProfileValues{Accounts: &accounts}
	if in.Status != "" {
		accountValues.IdentityStatus = &in.Status
	}
	candidates := []struct {
		block  string
		values ProfileValues
	}{{"x", x}, {"basic", basic}, {"accounts", accountValues}}
	result := []AuthorProfileBlock{}
	for _, item := range candidates {
		if _, err := validateProfileValues(item.block, item.values); err != nil {
			return nil, err
		}
		nonempty := false
		raw, _ := json.Marshal(item.values)
		var values map[string]any
		_ = json.Unmarshal(raw, &values)
		for _, value := range values {
			switch v := value.(type) {
			case string:
				nonempty = nonempty || v != ""
			case []any:
				nonempty = nonempty || len(v) > 0
			}
		}
		if !nonempty {
			continue
		}
		fields := []string{}
		for field, present := range profileFields(item.values) {
			if present {
				fields = append(fields, field)
			}
		}
		slices.Sort(fields)
		evidence := []ProfileEvidence{}
		for _, link := range in.EvidenceURLs {
			if link == "" {
				continue
			}
			status := "candidate"
			if in.Status == "verified" {
				status = "confirmed"
			}
			evidence = append(evidence, ProfileEvidence{URL: link, Title: "迁入的来源证据", Fields: fields, Status: status, Note: "保留导入资料中的核实状态。"})
		}
		result = append(result, AuthorProfileBlock{Block: item.block, Revision: in.Revision, Values: item.values, Evidence: evidence, Source: "imported"})
	}
	return result, nil
}

func canonicalXProfile(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "x.com", "www.x.com", "twitter.com", "www.twitter.com":
	default:
		return ""
	}
	account := strings.Trim(u.Path, "/")
	if account == "" {
		return ""
	}
	for _, character := range account {
		if character != '_' && (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return ""
		}
	}
	return strings.ToLower(account)
}

// Restore only fills absent blocks. Existing confirmed blocks, including
// deliberate empty values, remain authoritative; differing imported material
// becomes a reviewable candidate or remains in the complete raw archive.
func RestoreImportedAuthorProfile(ctx context.Context, q store.Querier, authorID, name, archiveID string, prepared *ImportedAuthorProfile) (ProfileImportResult, error) {
	result := ProfileImportResult{RestoredBlocks: []string{}, CandidateBlocks: []string{}, Issues: []string{}}
	if prepared == nil {
		return result, nil
	}
	author, err := store.Get[model.Author](ctx, q, "SELECT data FROM authors WHERE id=?", authorID)
	if err != nil {
		return result, err
	}
	now := time.Now().UTC()
	archive := ProfileArchive{ProfileArchiveSummary: ProfileArchiveSummary{ID: archiveID, AuthorID: authorID, AuthorName: name, Reason: "import", CreatedAt: now, Conflicts: []string{}}, Blocks: []AuthorProfileBlock{}, OriginalImport: prepared.original}
	for _, block := range prepared.blocks {
		block.AuthorID = authorID
		if block.Block == "x" && prepared.avatar != nil {
			image := prepared.avatar
			id := digest(image.body)
			source := profileString(prepared.profile.AvatarURL)
			block.Avatar = &ProfileAvatar{ID: id, URL: "/api/v1/profile-avatars/" + id, Origin: "import", SourceURL: source, FetchedURL: "", SourceHash: image.sourceHash, ContentHash: id, Width: image.width, Height: image.height, Bytes: len(image.body), FetchedAt: now}
			if _, err = q.ExecContext(ctx, "INSERT OR IGNORE INTO author_profile_avatars(id,created_at,image) VALUES(?,?,?)", id, dateKey(now), image.body); err != nil {
				return result, err
			}
		}
		archive.Blocks = append(archive.Blocks, block)
	}
	archive.BlockCount = len(archive.Blocks)
	if err = saveProfileArchive(ctx, q, archive); err != nil {
		return result, err
	}
	if author.Placeholder {
		result.Issues = append(result.Issues, "placeholder_profile_archived")
		return result, nil
	}
	if feed := profileString(prepared.profile.FeedURL); feed != "" {
		if _, valid := validateAvatarURL(feed); valid == nil {
			existing, e := savedAuthorUpdates(ctx, q, authorID)
			if e != nil {
				return result, e
			}
			if existing.Revision == 0 {
				existing.FeedURL = feed
				existing.Revision = 1
				if e = saveAuthorUpdates(ctx, q, existing); e != nil {
					return result, e
				}
			}
		}
	}
	for _, incoming := range archive.Blocks {
		current, err := profileBlock(ctx, q, authorID, incoming.Block)
		if err != nil {
			return result, err
		}
		if prepared.profile.Status == "verified" && current.Revision == 0 {
			incoming.Revision = 1
			incoming.UpdatedAt = &now
			if err = saveProfileBlock(ctx, q, incoming); err != nil {
				return result, err
			}
			result.RestoredBlocks = append(result.RestoredBlocks, incoming.Block)
			if incoming.Avatar != nil {
				result.AvatarRestored = true
			}
			continue
		}
		if incoming.Block == "x" && prepared.profile.Status == "verified" && current.Revision > 0 {
			changed := false
			if incoming.Avatar != nil && current.Avatar == nil && profileString(current.Values.AvatarSourceURL) == incoming.Avatar.SourceURL {
				current.Avatar = incoming.Avatar
				changed = true
				result.AvatarRestored = true
			}
			sameAccount := canonicalXProfile(profileString(current.Values.ProfileURL)) != "" && canonicalXProfile(profileString(current.Values.ProfileURL)) == canonicalXProfile(profileString(incoming.Values.ProfileURL))
			if sameAccount && current.Values.BiographyZh == nil && incoming.Values.BiographyZh != nil && profileString(current.Values.BiographyOriginal) == profileString(incoming.Values.BiographyOriginal) {
				current.Values.BiographyZh = incoming.Values.BiographyZh
				changed = true
			}
			if changed {
				current.Revision++
				current.UpdatedAt = &now
				if err = saveProfileBlock(ctx, q, current); err != nil {
					return result, err
				}
			}
		}
		if err = importProfileCandidate(ctx, q, authorID, archiveID+":main", incoming, current, &result); err != nil {
			return result, err
		}
	}
	if prepared.profile.Candidate != nil {
		blocks, err := convertImportedProfile(*prepared.profile.Candidate)
		if err != nil {
			return result, err
		}
		for _, incoming := range blocks {
			current, err := profileBlock(ctx, q, authorID, incoming.Block)
			if err != nil {
				return result, err
			}
			if err = importProfileCandidate(ctx, q, authorID, archiveID+":candidate", incoming, current, &result); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

func importProfileCandidate(ctx context.Context, q store.Querier, authorID, seed string, incoming, current AuthorProfileBlock, result *ProfileImportResult) error {
	incomingRaw, _ := json.Marshal(incoming.Values)
	currentRaw, _ := json.Marshal(current.Values)
	if string(incomingRaw) == string(currentRaw) {
		return nil
	}
	evidence := append([]ProfileEvidence{}, incoming.Evidence...)
	for index := range evidence {
		evidence[index].Status = "candidate"
		evidence[index].Note = "迁入资料，等待与当前档案核对。"
	}
	if len(evidence) == 0 {
		result.Issues = append(result.Issues, "unlinked_"+incoming.Block+"_preserved_in_archive")
		return nil
	}
	var count int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM author_profile_candidates WHERE author_id=? AND archive_id='' AND state='pending'", authorID).Scan(&count); err != nil {
		return err
	}
	if count >= 50 {
		result.Issues = append(result.Issues, "candidate_capacity_"+incoming.Block+"_preserved_in_archive")
		return nil
	}
	candidate := ProfileCandidate{ID: digest([]byte(seed + ":" + incoming.Block))[:32], AuthorID: authorID, Block: incoming.Block, BaseRevision: current.Revision, Values: incoming.Values, Evidence: evidence, State: "pending", CreatedAt: time.Now().UTC()}
	if err := saveProfileCandidate(ctx, q, candidate); err != nil {
		return err
	}
	result.CandidateBlocks = append(result.CandidateBlocks, incoming.Block)
	return nil
}
