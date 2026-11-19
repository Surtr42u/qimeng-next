// qimeng-legacy-convert is an offline, single-use bridge from a sealed PC v26
// database and explicit media roots to the native streaming backup format.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"
	_ "time/tzdata"

	"qimeng.local/next/server/internal/legacycold"
	"qimeng.local/next/server/internal/legacycold/activity"
	"qimeng.local/next/server/internal/legacycold/catalog"
)

const usage = "qimeng-legacy-convert --source-db <sealed-v26.sqlite> --roots <private-roots.json> --trash-root <old-trash-directory> --source-timezone <IANA-location> --output <new-directory> [--reserve-bytes <bytes>]"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("qimeng-legacy-convert", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // The flag parser may echo private path arguments.
	var opt legacycold.Options
	flags.StringVar(&opt.SourceDB, "source-db", "", "")
	flags.StringVar(&opt.Roots, "roots", "", "")
	flags.StringVar(&opt.TrashRoot, "trash-root", "", "")
	flags.StringVar(&opt.SourceTimezone, "source-timezone", "", "")
	flags.StringVar(&opt.Output, "output", "", "")
	flags.Int64Var(&opt.ReserveBytes, "reserve-bytes", 512<<20, "")
	err := flags.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Println(usage)
		return nil
	}
	if err != nil || flags.NArg() != 0 || opt.SourceDB == "" || opt.Roots == "" || opt.TrashRoot == "" || opt.Output == "" || opt.ReserveBytes <= 0 {
		return errors.New(usage)
	}
	if opt.SourceTimezone == "" || opt.SourceTimezone == "Local" {
		return errors.New("source-timezone must identify the old instance's IANA timezone")
	}
	location, err := time.LoadLocation(opt.SourceTimezone)
	if err != nil {
		return errors.New("source-timezone is not a recognized IANA timezone")
	}
	var stage string
	var last time.Time
	opt.Progress = func(current string, files int, bytes int64) {
		if stage != current || time.Since(last) >= 30*time.Second {
			fmt.Fprintf(os.Stderr, "%s files=%d bytes=%d\n", current, files, bytes)
			stage, last = current, time.Now()
		}
	}
	projectActivity := func(ctx context.Context, source *sql.DB, target *sql.Tx, sourceID string) error {
		return activity.Import(ctx, source, target, sourceID, location)
	}
	receipt, err := legacycold.Convert(ctx, opt, catalog.Prepare, projectActivity)
	if err != nil {
		return fmt.Errorf("Conversion stopped (%w). See the private coverage report if the output directory was created.", err)
	}
	return json.NewEncoder(os.Stdout).Encode(receipt)
}
