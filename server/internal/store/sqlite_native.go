//go:build android || qimeng_native_sqlite

package store

import (
	"database/sql"
	"errors"

	"github.com/mattn/go-sqlite3"
)

// Android's native SQLite uses the NDK's bionic filesystem interfaces inside
// the ordinary app sandbox. The build tag also lets CI exercise this driver on
// Linux using the same HTTP, backup, migration and recovery tests.
const (
	sqliteDriverName = "qimeng-native-sqlite"
	catalogOptions   = "_foreign_keys=on&" + researchOptions
	researchOptions  = "_busy_timeout=5000&_journal_mode=WAL&_synchronous=FULL"
	ownershipOptions = "_busy_timeout=0"
	snapshotOptions  = "mode=ro"
)

func init() {
	sql.Register(sqliteDriverName, &sqlite3.SQLiteDriver{ConnectHook: func(conn *sqlite3.SQLiteConn) error {
		if _, err := conn.Exec("PRAGMA trusted_schema=OFF", nil); err != nil {
			return err
		}
		if err := conn.RegisterFunc("qimeng_search_token", sqlSearchToken, true); err != nil {
			return err
		}
		return conn.RegisterFunc("qimeng_time_key", sqlTimeKey, true)
	}})
}

func DatabaseBusy(err error) bool {
	var failure sqlite3.Error
	return errors.As(err, &failure) && (failure.Code == sqlite3.ErrBusy || failure.Code == sqlite3.ErrLocked)
}
