package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"qimeng.local/next/server/internal/phonerelink"
)

func phoneRelink(args []string) error {
	var o phonerelink.Options
	flags := flag.NewFlagSet("qimeng-backup phone-relink", flag.ContinueOnError)
	flags.StringVar(&o.Archive, "archive", "", "Verified native .qnext file")
	flags.StringVar(&o.ArchiveSHA, "sha256", "", "Independent archive SHA-256")
	flags.StringVar(&o.Restored, "restored-db", "", "Fresh native restore's cold qimeng.sqlite")
	flags.StringVar(&o.RestoredSHA, "restored-sha256", "", "Accepted SHA-256 of the restored database")
	flags.StringVar(&o.RestoreReceipt, "restore-receipt", "", "Native restore JSON receipt")
	flags.StringVar(&o.Target, "target-db", "", "Consistent cold copy of the initialized empty phone catalog")
	flags.StringVar(&o.TargetSHA, "target-sha256", "", "Accepted SHA-256 of the target database")
	flags.StringVar(&o.Plan, "plan", "", "Owner's library and asset bindings")
	flags.StringVar(&o.PlanSHA, "plan-sha256", "", "Accepted SHA-256 of the plan bytes")
	flags.StringVar(&o.Inventory, "inventory", "", "Frozen phone physical-file evidence")
	flags.StringVar(&o.InventorySHA, "inventory-sha256", "", "Independent SHA-256 of the inventory bytes")
	flags.Int64Var(&o.TrashBudget, "trash-budget-bytes", 0, "Approved total byte budget for named missing native trash files")
	flags.StringVar(&o.Destination, "destination", "", "New private delivery directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	ctx, done := signal.NotifyContext(context.Background(), os.Interrupt)
	defer done()
	result, err := phonerelink.Run(ctx, o)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
