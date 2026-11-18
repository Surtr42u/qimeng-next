package store

import (
	"database/sql"
	"fmt"
	"time"

	"qimeng.local/next/server/internal/domain"
)

// Each connection gets the same durability and trust settings, including
// connections the pool opens after an earlier one was closed.
type DatabaseUse int

const (
	CatalogDatabase DatabaseUse = iota
	OwnershipDatabase
	ResearchDatabase
	SnapshotDatabase
)

func OpenDatabase(filename string, use DatabaseUse) (*sql.DB, error) {
	var options string
	switch use {
	case CatalogDatabase:
		options = catalogOptions
	case ResearchDatabase:
		options = researchOptions
	case OwnershipDatabase:
		options = ownershipOptions
	case SnapshotDatabase:
		options = snapshotOptions
	default:
		return nil, fmt.Errorf("unknown SQLite connection use %d", use)
	}
	db, err := sql.Open(sqliteDriverName, FileURI(filename)+"?"+options)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func sqlSearchToken(text, token any) int64 {
	content, _ := text.(string)
	term, _ := token.(string)
	if domain.SearchToken(content, term) {
		return 1
	}
	return 0
}

func sqlTimeKey(input any) (any, error) {
	text, ok := input.(string)
	if !ok || text == "" {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return nil, err
	}
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z"), nil
}
