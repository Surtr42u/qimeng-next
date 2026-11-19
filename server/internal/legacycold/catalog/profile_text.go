package catalog

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"
)

// JSON pointers here name only descriptive text in the documented v26 profile.
// The same leaf name somewhere else, including inside an array, grants no exception.
func biographyPointer(path []string) string {
	switch len(path) {
	case 1:
		if path[0] == "biography" {
			return "/biography"
		}
	case 2:
		if path[0] == "xProfile" && (path[1] == "biography" || path[1] == "biographyZh") {
			return "/xProfile/" + path[1]
		}
		if path[0] == "candidate" && path[1] == "biography" {
			return "/candidate/biography"
		}
	}
	return ""
}

// Old v26 defines these as whole URLs. A path segment, trailing parenthesis
// or query here belongs to the URL; prose punctuation rules do not apply.
func structuredProfileURL(path []string) bool {
	switch len(path) {
	case 1:
		return path[0] == "avatarUrl" || path[0] == "homepageUrl" || path[0] == "feedUrl"
	case 2:
		return path[0] == "xProfile" && path[1] == "url" ||
			path[0] == "candidate" && (path[1] == "avatarUrl" || path[1] == "homepageUrl") ||
			path[0] == "evidenceUrls" && path[1] == ""
	case 3:
		return path[0] == "updates" && path[1] == "" && path[2] == "url" ||
			path[0] == "candidate" && path[1] == "evidenceUrls" && path[2] == ""
	}
	return false
}

type textDisposition struct {
	Field             string `json:"field"`
	Action            string `json:"action"`
	RemovedParameters int    `json:"removedParameters"`
}

type jsonEdit struct {
	start, end int
	text       string
	decision   textDisposition
}

type jsonInspection struct {
	catalog *importer
	raw     json.RawMessage
	decoder *json.Decoder
	record  string
	edits   []jsonEdit
}

// Inspect every JSON token, including fields unknown to the projection. A
// duplicate object key cannot hide its first value in the source archive.
// Re-encode only the individual strings that require documented redaction.
func (i *importer) inspectJSON(raw json.RawMessage, record string) (json.RawMessage, []textDisposition, error) {
	if !json.Valid(raw) || !utf8.Valid(raw) {
		return nil, nil, sourceProblem("business_json_invalid")
	}
	d := jsonInspection{catalog: i, raw: raw, decoder: json.NewDecoder(bytes.NewReader(raw)), record: record}
	d.decoder.UseNumber()
	if err := d.visit(nil); err != nil {
		return nil, nil, err
	}
	if len(d.edits) == 0 {
		return raw, nil, nil
	}
	var out bytes.Buffer
	last := 0
	decisions := make([]textDisposition, 0, len(d.edits))
	for _, e := range d.edits {
		out.Write(raw[last:e.start])
		body, err := json.Marshal(e.text)
		if err != nil {
			return nil, nil, err
		}
		out.Write(body)
		last = e.end
		decisions = append(decisions, e.decision)
	}
	out.Write(raw[last:])
	return out.Bytes(), decisions, nil
}

func (d *jsonInspection) visit(path []string) error {
	start := int(d.decoder.InputOffset())
	token, err := d.decoder.Token()
	if err != nil {
		return sourceProblem("business_json_invalid")
	}
	switch v := token.(type) {
	case string:
		if d.record == "creator_profile" && len(path) == 1 && path[0] == "avatarData" {
			return nil // decoded and validated as an image before any commit
		}
		if v != "" && d.record == "creator_profile" && structuredProfileURL(path) {
			_, _, err := d.catalog.publicReference(v, strictText, 0)
			return err
		}
		pointer := ""
		policy := strictText
		if d.record == "creator_profile" {
			pointer = biographyPointer(path)
			if pointer != "" {
				policy = biographyText
			}
		} else if d.record == "imported_txt_sources" && len(path) == 2 && path[0] == "" && path[1] == "content" {
			policy = fragmentText
		}
		text, count, err := d.catalog.inspectText(v, policy)
		if err != nil || count == 0 {
			return err
		}
		end := int(d.decoder.InputOffset())
		// InputOffset before the token can include the colon and whitespace
		// after the object key; the first quote starts this scalar.
		start += bytes.IndexByte(d.raw[start:end], '"')
		d.edits = append(d.edits, jsonEdit{start, end, text, textDisposition{pointer, "remove_sensitive_url_query_parameters", count}})
	case json.Delim:
		seen := map[string]bool{}
		for d.decoder.More() {
			key := ""
			if v == '{' {
				t, err := d.decoder.Token()
				if err != nil {
					return sourceProblem("business_json_invalid")
				}
				key = t.(string) // json.Valid and object position guarantee a key
				if seen[key] {
					return sourceProblem("business_json_invalid")
				}
				seen[key] = true
				if sensitiveKey(key) {
					return errPrivate
				}
				if err := d.catalog.portableText(key); err != nil {
					return err
				}
			}
			if err := d.visit(append(path, key)); err != nil {
				return err
			}
		}
		if _, err := d.decoder.Token(); err != nil {
			return sourceProblem("business_json_invalid")
		}
	}
	return nil
}
