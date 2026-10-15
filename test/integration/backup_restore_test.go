//go:build integration

package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"labnexus/internal/config"
	"labnexus/internal/database"
)

// TestBackupRestoreLifecycle 用隔离数据库验证 A2 备份恢复验收标准:
// 备份 → 修改数据与文件 → 恢复 → 校验;并覆盖未确认拒绝与损坏包拒绝。
func TestBackupRestoreLifecycle(t *testing.T) {
	baseCfg := config.Load()
	admin, err := database.New(baseCfg)
	if err != nil {
		t.Skipf("集成环境未就绪(先 `make up` 启动 Postgres/Redis): %v", err)
	}
	defer closeDB(admin)

	dbName := "labnexus_backup_" + time.Now().Format("20060102150405") + "_" + uuid.NewString()[:8]
	require.NoError(t, admin.Exec("CREATE DATABASE "+quoteIdent(dbName)).Error)
	defer func() {
		_ = admin.Exec("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = ?", dbName).Error
		_ = admin.Exec("DROP DATABASE IF EXISTS " + quoteIdent(dbName)).Error
	}()

	cfg := *baseCfg
	cfg.DBName = dbName
	require.NoError(t, database.MigrateUp(context.Background(), &cfg, "../../migrations"))

	// 标记数据:一行用户
	db, err := database.New(&cfg)
	require.NoError(t, err)
	require.NoError(t, db.Exec(
		`INSERT INTO users (id, username, display_name, password_hash, role)
		 VALUES (?, 'marker_user', 'Marker Original', 'hash', 'student')`,
		uuid.NewString(),
	).Error)
	closeDB(db)

	// 临时上传目录与文件
	work := t.TempDir()
	uploadsDir := filepath.Join(work, "uploads")
	require.NoError(t, os.MkdirAll(filepath.Join(uploadsDir, "2026"), 0o755))
	markerFile := filepath.Join(uploadsDir, "2026", "hello.txt")
	require.NoError(t, os.WriteFile(markerFile, []byte("backup-content"), 0o644))

	backupDir := filepath.Join(work, "backups")
	env := append(os.Environ(),
		"DB_HOST="+cfg.DBHost,
		"DB_PORT="+cfg.DBPort,
		"DB_USER="+cfg.DBUser,
		"DB_PASSWORD="+cfg.DBPassword,
		"DB_NAME="+cfg.DBName,
		"UPLOADS_DIR="+uploadsDir,
		"BACKUP_DIR="+backupDir,
	)

	// 1) 生成备份包
	out, err := runScript(t, env, "scripts/backup.sh")
	require.NoError(t, err, "备份失败: %s", out)
	require.Contains(t, out, "备份完成")

	matches, err := filepath.Glob(filepath.Join(backupDir, "labnexus-*.tar.gz"))
	require.NoError(t, err)
	require.Len(t, matches, 1, "应生成唯一备份包")
	backupFile := matches[0]

	// 2) 破坏当前状态:改数据库、删文件、加新文件
	db, err = database.New(&cfg)
	require.NoError(t, err)
	require.NoError(t, db.Exec(
		`UPDATE users SET display_name = 'Marker Changed' WHERE username = 'marker_user'`,
	).Error)
	require.NoError(t, db.Exec(
		`INSERT INTO users (id, username, display_name, password_hash, role)
		 VALUES (?, 'extra_user', 'Extra', 'hash', 'student')`, uuid.NewString(),
	).Error)
	closeDB(db)
	require.NoError(t, os.Remove(markerFile))
	require.NoError(t, os.WriteFile(filepath.Join(uploadsDir, "2026", "extra.txt"), []byte("extra"), 0o644))

	// 3) 未确认时必须拒绝
	out, err = runScript(t, env, "scripts/restore.sh", backupFile)
	require.Error(t, err, "未确认却执行成功: %s", out)
	require.Contains(t, out, "拒绝执行")

	// 4) 正式恢复
	confirmEnv := append(env, "CONFIRM=yes")
	out, err = runScript(t, confirmEnv, "scripts/restore.sh", backupFile)
	require.NoError(t, err, "恢复失败: %s", out)
	require.Contains(t, out, "恢复完成")

	// 5) 校验数据库与文件均回到备份时刻
	db, err = database.New(&cfg)
	require.NoError(t, err)
	var display string
	require.NoError(t, db.Raw(
		`SELECT display_name FROM users WHERE username = 'marker_user'`,
	).Scan(&display).Error)
	require.Equal(t, "Marker Original", display, "用户数据未恢复")
	var extraCount int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM users WHERE username = 'extra_user'`).Scan(&extraCount).Error)
	require.Equal(t, int64(0), extraCount, "备份后新增的数据应被覆盖")
	closeDB(db)

	content, err := os.ReadFile(markerFile)
	require.NoError(t, err, "标记文件应被恢复")
	require.Equal(t, "backup-content", string(content))
	_, statErr := os.Stat(filepath.Join(uploadsDir, "2026", "extra.txt"))
	require.True(t, os.IsNotExist(statErr), "备份后新增的文件应被移除")

	// 6) 损坏的备份包必须在写库前被拒绝
	corrupt := tamperBackup(t, backupFile, work)
	out, err = runScript(t, confirmEnv, "scripts/restore.sh", corrupt)
	require.Error(t, err, "损坏备份却恢复成功: %s", out)
	require.Contains(t, out, "校验失败")
}

// tamperBackup 复制备份包并篡改 uploads.tar.gz,返回新包路径。
func tamperBackup(t *testing.T, backupFile, work string) string {
	t.Helper()
	extractDir := filepath.Join(work, "corrupt")
	require.NoError(t, os.MkdirAll(extractDir, 0o755))
	require.NoError(t, exec.Command("tar", "-xzf", backupFile, "-C", extractDir).Run())

	badFile := filepath.Join(extractDir, "uploads.tar.gz")
	require.NoError(t, os.WriteFile(badFile, []byte("tampered-not-a-real-archive"), 0o644))

	corrupt := filepath.Join(work, "corrupt-backup.tar.gz")
	cmd := exec.Command("tar", "-czf", corrupt, "-C", extractDir, "MANIFEST.sha256", "database.dump", "uploads.tar.gz")
	require.NoError(t, cmd.Run())
	return corrupt
}

// runScript 以项目根为基准执行 scripts/ 下的脚本。
func runScript(t *testing.T, env []string, script string, args ...string) (string, error) {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	path := filepath.Join(wd, "..", "..", script)
	cmd := exec.Command("bash", append([]string{path}, args...)...)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}
