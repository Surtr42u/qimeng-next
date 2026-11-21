// qimeng-backup is an offline restore boundary. It never starts the HTTP service
// and cannot overwrite an existing instance or select model-provided host roots.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"qimeng.local/next/server/internal/service"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "Backup operation failed; the source is unchanged. Incomplete restore directories remain marked.")
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("expected verify or restore")
	}
	flags := flag.NewFlagSet("qimeng-backup", flag.ContinueOnError)
	archive := flags.String("archive", "", "Path to a .qnext archive (records-only or an older complete archive)")
	destination := flags.String("destination", "", "New isolated data directory (must not exist)")
	hash := flags.String("sha256", "", "Expected archive SHA-256 from the source service")
	if e := flags.Parse(os.Args[2:]); e != nil {
		return e
	}
	if flags.NArg() != 0 || *archive == "" {
		return fmt.Errorf("invalid backup arguments")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var result service.RestoreResult
	var e error
	switch os.Args[1] {
	case "verify":
		result, e = service.VerifyBackup(ctx, *archive, *hash)
	case "restore":
		result, e = service.RestoreBackup(ctx, service.RestoreOptions{ArchivePath: *archive, Destination: *destination, ExpectedSHA256: *hash})
	default:
		return fmt.Errorf("unknown backup command")
	}
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
