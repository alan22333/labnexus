package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/pressly/goose/v3"

	"labnexus/internal/config"
)

var coreTables = []string{
	"users", "spaces", "documents", "resources", "projects", "task_types", "accounts",
}

// ErrBaselineAlreadyVersioned 表示数据库已经有 goose 版本记录,禁止再次执行基线标记。
var ErrBaselineAlreadyVersioned = errors.New("database already has migration version records")

// MigrateUp 应用全部待执行迁移。
func MigrateUp(ctx context.Context, cfg *config.Config, dir string) error {
	return withSQLDB(cfg, func(db *sql.DB) error {
		if err := validateMigrationDir(dir); err != nil {
			return err
		}
		if err := goose.UpContext(ctx, db, dir); err != nil {
			return fmt.Errorf("goose up: %w", err)
		}
		return nil
	})
}

// MigrateDown 回滚最近一版迁移。
func MigrateDown(ctx context.Context, cfg *config.Config, dir string) error {
	return withSQLDB(cfg, func(db *sql.DB) error {
		if err := validateMigrationDir(dir); err != nil {
			return err
		}
		if err := goose.DownContext(ctx, db, dir); err != nil {
			return fmt.Errorf("goose down: %w", err)
		}
		return nil
	})
}

// MigrateStatus 输出 goose 版本状态表。
func MigrateStatus(ctx context.Context, cfg *config.Config, dir string) error {
	return withSQLDB(cfg, func(db *sql.DB) error {
		if err := validateMigrationDir(dir); err != nil {
			return err
		}
		if err := goose.StatusContext(ctx, db, dir); err != nil {
			return fmt.Errorf("goose status: %w", err)
		}
		return nil
	})
}

// MigrationVersion 返回数据库当前已应用的最高迁移版本。
func MigrationVersion(ctx context.Context, cfg *config.Config) (int64, error) {
	var version int64
	err := withSQLDB(cfg, func(db *sql.DB) error {
		var err error
		version, err = goose.GetDBVersionContext(ctx, db)
		if err != nil {
			return fmt.Errorf("goose version: %w", err)
		}
		return nil
	})
	return version, err
}

// MigrateBaseline 处理从 AutoMigrate 迁移到 goose 的一次性升级。
// 空库直接执行全部迁移;已有完整业务表的库标记版本 1 后执行增量迁移。
func MigrateBaseline(ctx context.Context, cfg *config.Config, dir string) error {
	return withSQLDB(cfg, func(db *sql.DB) error {
		if err := validateMigrationDir(dir); err != nil {
			return err
		}

		records, err := migrationRecordCount(ctx, db)
		if err != nil {
			return err
		}
		if records > 0 {
			return ErrBaselineAlreadyVersioned
		}

		state, err := coreSchemaState(ctx, db)
		if err != nil {
			return err
		}
		switch state {
		case coreSchemaEmpty:
			if err := goose.UpContext(ctx, db, dir); err != nil {
				return fmt.Errorf("baseline empty database: %w", err)
			}
			return nil
		case coreSchemaPartial:
			return errors.New("database contains a partial LabNexus schema; refusing to stamp baseline")
		case coreSchemaExisting:
			if err := stampBaselineVersion(ctx, db); err != nil {
				return err
			}
			if err := goose.UpContext(ctx, db, dir); err != nil {
				return fmt.Errorf("apply migrations after baseline: %w", err)
			}
			return nil
		default:
			return fmt.Errorf("unknown schema state: %d", state)
		}
	})
}

type coreSchema int

const (
	coreSchemaEmpty coreSchema = iota
	coreSchemaExisting
	coreSchemaPartial
)

func coreSchemaState(ctx context.Context, db *sql.DB) (coreSchema, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name = ANY($1)
	`, coreTables)
	if err != nil {
		return coreSchemaEmpty, fmt.Errorf("inspect database schema: %w", err)
	}
	defer rows.Close()

	found := 0
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return coreSchemaEmpty, err
		}
		found++
	}
	if err := rows.Err(); err != nil {
		return coreSchemaEmpty, err
	}
	switch found {
	case 0:
		return coreSchemaEmpty, nil
	case len(coreTables):
		return coreSchemaExisting, nil
	default:
		return coreSchemaPartial, nil
	}
}

func migrationRecordCount(ctx context.Context, db *sql.DB) (int64, error) {
	var exists bool
	if err := db.QueryRowContext(ctx, "SELECT to_regclass('goose_db_version') IS NOT NULL").Scan(&exists); err != nil {
		return 0, fmt.Errorf("check goose version table: %w", err)
	}
	if !exists {
		return 0, nil
	}
	var count int64
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM goose_db_version").Scan(&count); err != nil {
		return 0, fmt.Errorf("count goose version records: %w", err)
	}
	return count, nil
}

func stampBaselineVersion(ctx context.Context, db *sql.DB) error {
	if _, err := goose.EnsureDBVersionContext(ctx, db); err != nil && !errors.Is(err, goose.ErrNoNextVersion) {
		return fmt.Errorf("ensure goose version table: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO goose_db_version (version_id, is_applied, tstamp)
		VALUES (1, TRUE, now())
	`); err != nil {
		return fmt.Errorf("stamp baseline version: %w", err)
	}
	return nil
}

func withSQLDB(cfg *config.Config, fn func(*sql.DB) error) error {
	gormDB, err := New(cfg)
	if err != nil {
		return err
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return fmt.Errorf("get sql db: %w", err)
	}
	defer sqlDB.Close()

	configureGoose()
	return fn(sqlDB)
}

func configureGoose() {
	_ = goose.SetDialect("postgres")
	goose.SetTableName("goose_db_version")
	goose.SetBaseFS(nil)
}

func validateMigrationDir(dir string) error {
	if dir == "" {
		return errors.New("migration directory is empty")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("migration directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("migration path %q is not a directory", dir)
	}
	return nil
}
