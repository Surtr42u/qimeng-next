// qimeng-merge-selected applies an explicit owner decision to a fresh restore.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	_ "time/tzdata"

	"qimeng.local/next/server/internal/assetmerge"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Selected image merge stopped at", assetmerge.FailurePhase(err))
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return errors.New("expected check or apply")
	}
	var o assetmerge.Options
	flags := flag.NewFlagSet("qimeng-merge-selected "+os.Args[1], flag.ContinueOnError)
	flags.StringVar(&o.Archive, "archive", "", "Sealed .qnext used for this restore (absolute path)")
	flags.StringVar(&o.SHA256, "sha256", "", "Independently verified archive SHA-256")
	flags.StringVar(&o.Stage, "stage", "", "New, never-served offline qimeng-backup restore")
	flags.StringVar(&o.RestoreReceipt, "restore-receipt", "", "Private UTF-8 JSON from qimeng-backup restore")
	flags.StringVar(&o.Plan, "plan", "", "Private owner-reviewed group JSON")
	flags.StringVar(&o.PlanSHA256, "plan-sha256", "", "SHA-256 of the exact authorized JSON file")
	flags.StringVar(&o.CatalogSHA256, "catalog-sha256", "", "Accepted SHA-256 of this stopped restored SQLite catalog (required for apply and replay)")
	flags.StringVar(&o.Preimage, "preimage", "", "New private preimage and receipt directory next to the stage")
	flags.StringVar(&o.Destination, "destination", "", "New final directory next to the stage")
	flags.Int64Var(&o.ReserveBytes, "reserve-bytes", 512<<20, "Keep this many bytes free during private copies")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	result, err := assetmerge.Run(ctx, os.Args[1], o)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
