package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"qimeng.local/next/server/internal/store"
)

type AcquisitionVocabulary struct {
	Revision    int64    `json:"revision"`
	Sources     []string `json:"sources"`
	Suggestions []string `json:"suggestions"`
}
type AcquisitionVocabularyUpdate struct {
	ExpectedRevision int64     `json:"expectedRevision"`
	Sources          *[]string `json:"sources"`
}

func acquisitionSuggestions(sources []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, source := range sources {
		for _, word := range strings.Fields(source) {
			if !seen[word] {
				seen[word] = true
				out = append(out, word)
			}
		}
	}
	return out
}
func (s *Service) AcquisitionVocabulary(ctx context.Context) (AcquisitionVocabulary, error) {
	value, e := store.Get[AcquisitionVocabulary](ctx, s.DB.DB, "SELECT value FROM settings WHERE key='acquisition_vocabulary'")
	if errors.Is(e, sql.ErrNoRows) {
		value = AcquisitionVocabulary{Revision: 1, Sources: []string{}}
	} else if e != nil {
		return value, e
	}
	value.Suggestions = acquisitionSuggestions(value.Sources)
	return value, nil
}
func (s *Service) UpdateAcquisitionVocabulary(ctx context.Context, w *Write, in AcquisitionVocabularyUpdate) (AcquisitionVocabulary, error) {
	if e := Authorize(w.Principal, "admin", ""); e != nil {
		return AcquisitionVocabulary{}, e
	}
	current, e := s.AcquisitionVocabulary(ctx)
	if e != nil {
		return current, e
	}
	if current.Revision != in.ExpectedRevision {
		return current, Conflict(current.Revision)
	}
	if in.Sources == nil {
		return current, Fail(400, "sources_required", "请提供完整获取渠道数组，清空时使用 []。")
	}
	sources, e := cleanStrings(*in.Sources, 32, 500)
	if e != nil {
		return current, e
	}
	current.Sources = sources
	current.Suggestions = acquisitionSuggestions(sources)
	current.Revision++
	raw, e := store.Encode(current)
	if e != nil {
		return current, e
	}
	q, e := w.Q()
	if e != nil {
		return current, e
	}
	_, e = q.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES('acquisition_vocabulary',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", raw)
	return current, e
}
