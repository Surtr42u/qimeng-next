//go:build !android && !qimeng_native_sqlite

package store

import (
	"database/sql/driver"
	"errors"

	"modernc.org/sqlite"
)

const (
	sqliteDriverName = "sqlite"
	catalogOptions   = "_pragma=foreign_keys(1)&" + researchOptions
	researchOptions  = "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)"
	ownershipOptions = "_pragma=busy_timeout(0)"
	snapshotOptions  = "mode=ro&_pragma=trusted_schema(0)"
)

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("qimeng_search_token", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		return sqlSearchToken(args[0], args[1]), nil
	})
	sqlite.MustRegisterDeterministicScalarFunction("qimeng_time_key", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		return sqlTimeKey(args[0])
	})
}

func DatabaseBusy(err error) bool {
	var failure *sqlite.Error
	return errors.As(err, &failure) && (failure.Code()&255 == 5 || failure.Code()&255 == 6)
}
