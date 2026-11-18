package researchprovider

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
)

type identifier string

func (id *identifier) UnmarshalJSON(raw []byte) error {
	value := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
	}
	if digits.MatchString(value) {
		*id = identifier(value)
	}
	return nil
}
func short(value string, n int) string {
	out := []rune(strings.Join(strings.FieldsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " "))
	if len(out) > n {
		out = out[:n]
	}
	return string(out)
}
func links(values []string) []string {
	var result []string
	for _, value := range values {
		if link := cleanLink(value); link != "" && !slices.Contains(result, link) {
			result = append(result, link)
			if len(result) == 5 {
				break
			}
		}
	}
	return result
}
func HTTPState(code int) string {
	switch code {
	case http.StatusOK:
		return "ok"
	case 404, 410:
		return "not_found"
	case 401, 403, 451:
		return "blocked"
	case 429:
		return "rate_limited"
	default:
		return "unavailable"
	}
}
func Parse(q Query, raw []byte) (string, []Item) {
	var items []Item
	state := "ok"
	switch q.Name {
	case "fxtwitter":
		state, items = fx(q, raw)
	case "pixiv":
		state, items = pixiv(q, raw)
	case "steam":
		state, items = steam(q, raw)
	case "yandere":
		state, items = yandere(q, raw)
	case "e621":
		state, items = e621(q, raw)
	case "wayback":
		state, items = wayback(q, raw)
	case "bing":
		state, items = bing(q, raw)
	default:
		state = "unavailable"
	}
	if len(items) > q.Limit {
		items = items[:q.Limit]
	}
	if state == "ok" && len(items) == 0 {
		state = "not_found"
	}
	if items == nil {
		items = []Item{}
	}
	return state, items
}

type fxUser struct {
	ID      identifier `json:"id"`
	Name    string     `json:"name"`
	Handle  string     `json:"screen_name"`
	Website struct {
		URL string `json:"url"`
	} `json:"website"`
}

func fx(q Query, raw []byte) (string, []Item) {
	var result struct {
		Code   int    `json:"code"`
		User   fxUser `json:"user"`
		Status struct {
			ID      identifier `json:"id"`
			Text    string     `json:"text"`
			Author  fxUser     `json:"author"`
			Created string     `json:"created_at"`
		} `json:"status"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return "unavailable", nil
	}
	if state := HTTPState(result.Code); state != "ok" {
		return state, nil
	}
	if q.Operation == "author" {
		u := result.User
		if u.ID == "" || !handle.MatchString(u.Handle) || !strings.EqualFold(u.Handle, q.Target) {
			return "unavailable", nil
		}
		return "ok", []Item{{ID: string(u.ID), Title: short(u.Name, 200), Author: u.Handle, AuthorID: string(u.ID), URL: "https://x.com/" + u.Handle, Sources: links([]string{u.Website.URL})}}
	}
	p := result.Status
	if string(p.ID) != q.Target || !handle.MatchString(p.Author.Handle) || p.Author.ID == "" {
		return "unavailable", nil
	}
	return "ok", []Item{{ID: string(p.ID), URL: "https://x.com/" + p.Author.Handle + "/status/" + string(p.ID),
		Author: p.Author.Handle, AuthorID: string(p.Author.ID), Title: short(p.Text, 240), PublishedAt: short(p.Created, 64)}}
}
func pixiv(q Query, raw []byte) (string, []Item) {
	var result struct {
		Error *bool           `json:"error"`
		Body  json.RawMessage `json:"body"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Error == nil {
		return "unavailable", nil
	}
	// Pixiv's generic error envelope can also mean a login/visibility block.
	if *result.Error {
		return "unavailable", nil
	}
	var obj struct {
		UserID   identifier `json:"userId"`
		ID       identifier `json:"id"`
		Name     string     `json:"name"`
		UserName string     `json:"userName"`
		Title    string     `json:"title"`
		Created  string     `json:"createDate"`
	}
	if json.Unmarshal(result.Body, &obj) != nil {
		return "unavailable", nil
	}
	if q.Operation == "author" {
		if string(obj.UserID) != q.Target || obj.Name == "" {
			return "unavailable", nil
		}
		return "ok", []Item{{ID: q.Target, AuthorID: q.Target, Author: short(obj.Name, 200), URL: "https://www.pixiv.net/users/" + q.Target}}
	}
	if string(obj.ID) != q.Target || obj.UserID == "" {
		return "unavailable", nil
	}
	return "ok", []Item{{ID: q.Target, Title: short(obj.Title, 240), Author: short(obj.UserName, 200), AuthorID: string(obj.UserID), PublishedAt: short(obj.Created, 64), URL: "https://www.pixiv.net/artworks/" + q.Target}}
}
func steam(q Query, raw []byte) (string, []Item) {
	var x struct {
		Response struct {
			Details []struct {
				ID      identifier `json:"publishedfileid"`
				Creator identifier `json:"creator"`
				Result  int        `json:"result"`
				Title   string     `json:"title"`
				Created int64      `json:"time_created"`
			} `json:"publishedfiledetails"`
		} `json:"response"`
	}
	if json.Unmarshal(raw, &x) != nil || x.Response.Details == nil {
		return "unavailable", nil
	}
	if len(x.Response.Details) == 0 {
		return "not_found", nil
	}
	v := x.Response.Details[0]
	if v.Result == 9 || v.Result == 15 {
		if v.Result == 15 {
			return "blocked", nil
		}
		return "not_found", nil
	}
	if v.Result != 1 || string(v.ID) != q.Target {
		return "unavailable", nil
	}
	result := Item{ID: q.Target, AuthorID: string(v.Creator), Title: short(v.Title, 240), URL: "https://steamcommunity.com/sharedfiles/filedetails/?id=" + q.Target}
	if v.Created > 0 {
		result.PublishedAt = time.Unix(v.Created, 0).UTC().Format(time.RFC3339)
	}
	if v.Creator != "" {
		result.Sources = []string{"https://steamcommunity.com/profiles/" + string(v.Creator)}
	}
	return "ok", []Item{result}
}
func yandere(q Query, raw []byte) (string, []Item) {
	var rows []struct {
		ID     identifier      `json:"id"`
		Name   string          `json:"name"`
		Source string          `json:"source"`
		URLs   json.RawMessage `json:"urls"`
	}
	if json.Unmarshal(raw, &rows) != nil || rows == nil {
		return "unavailable", nil
	}
	out := []Item{}
	for _, row := range rows {
		if row.ID == "" {
			return "unavailable", nil
		}
		item := Item{ID: string(row.ID), URL: "https://yande.re/post/show/" + string(row.ID), Sources: links([]string{row.Source})}
		if q.Operation == "artist" {
			item.URL = "https://yande.re/artist/show/" + string(row.ID)
			item.Author = short(row.Name, 200)
			var list []string
			if json.Unmarshal(row.URLs, &list) != nil {
				var text string
				if json.Unmarshal(row.URLs, &text) == nil {
					list = strings.Fields(text)
				}
			}
			item.Sources = links(list)
		}
		out = append(out, item)
		if len(out) >= q.Limit {
			break
		}
	}
	return "ok", out
}

type e621Post struct {
	ID   identifier `json:"id"`
	Tags struct {
		Artist []string `json:"artist"`
	} `json:"tags"`
	Sources []string `json:"sources"`
	Created string   `json:"created_at"`
}

func e621(q Query, raw []byte) (string, []Item) {
	var body struct {
		Post  *e621Post  `json:"post"`
		Posts []e621Post `json:"posts"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return "unavailable", nil
	}
	rows := body.Posts
	if q.Operation == "post" {
		if body.Post == nil || string(body.Post.ID) != q.Target {
			return "unavailable", nil
		}
		rows = []e621Post{*body.Post}
	} else if rows == nil {
		return "unavailable", nil
	}
	out := []Item{}
	for _, r := range rows {
		if r.ID == "" {
			return "unavailable", nil
		}
		authors := r.Tags.Artist[:min(len(r.Tags.Artist), 5)]
		out = append(out, Item{ID: string(r.ID), Author: short(strings.Join(authors, ", "), 200), URL: "https://e621.net/posts/" + string(r.ID), PublishedAt: short(r.Created, 64), Sources: links(r.Sources)})
		if len(out) >= q.Limit {
			break
		}
	}
	return "ok", out
}
func wayback(q Query, raw []byte) (string, []Item) {
	var rows [][]string
	if json.Unmarshal(raw, &rows) != nil || rows == nil {
		return "unavailable", nil
	}
	if len(rows) == 0 {
		return "not_found", nil
	}
	if !slices.Equal(rows[0], []string{"timestamp", "original", "statuscode"}) {
		return "unavailable", nil
	}
	out := []Item{}
	for _, r := range rows[1:] {
		if len(r) != 3 || r[2] != "200" {
			continue
		}
		when, err := time.Parse("20060102150405", r[0])
		if err != nil || cleanLink(r[1]) != q.Target {
			continue
		}
		out = append(out, Item{ID: r[0], URL: "https://web.archive.org/web/" + r[0] + "/" + q.Target, PublishedAt: when.UTC().Format(time.RFC3339), Sources: []string{q.Target}})
		if len(out) >= q.Limit {
			break
		}
	}
	return "ok", out
}
func bingLink(value string) string {
	u := publicURL(value)
	if u == nil {
		return ""
	}
	if u.Host == "bing.com" || u.Host == "www.bing.com" {
		encoded := u.Query().Get("u")
		if u.Path != "/ck/a" || !strings.HasPrefix(encoded, "a1") {
			return ""
		}
		decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(encoded[2:], "="))
		if err != nil {
			return ""
		}
		value = string(decoded)
	}
	return cleanLink(value)
}
func bing(q Query, raw []byte) (string, []Item) {
	z := html.NewTokenizer(bytes.NewReader(raw))
	out := []Item{}
	var inResult, inHeading, inLink, noResults bool
	var href string
	var words strings.Builder
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break
		}
		token := z.Token()
		switch kind {
		case html.StartTagToken:
			if token.Data == "li" {
				inResult = false
				inHeading = false
				inLink = false
				for _, att := range token.Attr {
					if att.Key == "class" {
						classes := strings.Fields(att.Val)
						inResult = slices.Contains(classes, "b_algo")
						noResults = noResults || slices.Contains(classes, "b_no")
					}
				}
			}
			if token.Data == "h2" && inResult {
				inHeading = true
			}
			if token.Data == "a" && inHeading {
				href = ""
				for _, att := range token.Attr {
					if att.Key == "href" {
						href = bingLink(att.Val)
					}
				}
				inLink = href != ""
				words.Reset()
			}
		case html.TextToken:
			if inLink && words.Len() < 2048 {
				words.WriteString(token.Data)
			}
		case html.EndTagToken:
			if token.Data == "a" && inLink {
				if title := short(words.String(), 240); title != "" {
					out = append(out, Item{URL: href, Title: title})
				}
				inLink = false
			}
			if token.Data == "h2" {
				inHeading = false
				inLink = false
			}
			if token.Data == "li" {
				inResult = false
				inHeading = false
				inLink = false
			}
		}
		if len(out) >= q.Limit {
			break
		}
	}
	if len(out) == 0 && !noResults {
		return "unavailable", nil
	}
	return "ok", out
}
