package store

import (
 "net/url"
 "path/filepath"
 "strings"
 "testing"
)

func TestFileURIKeepsWindowsDriveInPathAndEscapesFilename(t *testing.T){
 uri:=FileURI("C:/synthetic/database #?.sqlite");parsed,e:=url.Parse(uri);if e!=nil{t.Fatal(e)}
 if parsed.Host!=""||parsed.Path!="/C:/synthetic/database #?.sqlite"||parsed.RawQuery!=""||parsed.Fragment!=""||!strings.HasPrefix(uri,"file:/C:/"){t.Fatalf("unsafe SQLite file URI: %q",uri)}
}
func TestDatabasePathWithURICharacters(t *testing.T){
 db,e:=Open(filepath.Join(t.TempDir(),"business #%.sqlite"));if e!=nil{t.Fatal(e)};defer db.DB.Close()
 var version int;if e=db.DB.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version);e!=nil||version<1{t.Fatalf("migration: %d %v",version,e)}
}
