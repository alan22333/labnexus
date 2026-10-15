// Command migrate 执行 LabNexus 版本化数据库迁移。
package main

import (
	"context"
	"fmt"
	"os"

	"labnexus/internal/config"
	"labnexus/internal/database"
)

func main() {
	if len(os.Args) != 2 {
		usage()
		os.Exit(2)
	}
	cfg := config.Load()
	ctx := context.Background()

	var err error
	switch os.Args[1] {
	case "up":
		err = database.MigrateUp(ctx, cfg, cfg.MigrationsDir)
	case "down":
		err = database.MigrateDown(ctx, cfg, cfg.MigrationsDir)
	case "status":
		err = database.MigrateStatus(ctx, cfg, cfg.MigrationsDir)
	case "baseline":
		err = database.MigrateBaseline(ctx, cfg, cfg.MigrationsDir)
	case "version":
		var version int64
		version, err = database.MigrationVersion(ctx, cfg)
		if err == nil {
			fmt.Printf("current migration version: %d\n", version)
		}
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate %s failed: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: go run ./cmd/migrate <up|down|status|baseline|version>")
}
