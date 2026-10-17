//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"labnexus/internal/demo"
	"labnexus/internal/user"
)

// TestDemoSeedDeterministicAndHighFidelity 校验演示数据可重复执行且覆盖全功能。
// 依据规格 docs/specs/dev-demo-data.md §4。
func TestDemoSeedDeterministicAndHighFidelity(t *testing.T) {
	db := connectDB(t)
	defer closeDB(db)
	ctx := context.Background()

	first, err := demo.Seed(ctx, db, t.TempDir())
	require.NoError(t, err, "首次种子失败")
	second, err := demo.Seed(ctx, db, t.TempDir())
	require.NoError(t, err, "二次种子失败")
	assert.Equal(t, first, second, "重复执行结果应一致(幂等/确定性)")

	// 账号与角色
	assert.GreaterOrEqual(t, second.Users, 6)
	var admin user.User
	require.NoError(t, db.First(&admin, "username = ?", demo.AdminUsername).Error)
	assert.Equal(t, user.RoleAdmin, admin.Role)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(demo.AdminPassword)),
		"演示管理员密码应可用")

	var supervisor user.User
	require.NoError(t, db.First(&supervisor, "username = ?", demo.SupervisorUser).Error)
	assert.Equal(t, user.RoleSupervisor, supervisor.Role)

	// 社区:公开帖 + 私密笔记 + 评论 + 点赞
	assert.GreaterOrEqual(t, second.PublicDocuments, 1)
	assert.GreaterOrEqual(t, second.PrivateDocuments, 1)
	assert.GreaterOrEqual(t, second.Comments, 1)
	assert.GreaterOrEqual(t, second.Reactions, 1)

	// 资源:link + file
	var resTypes []string
	require.NoError(t, db.Table("resources").Distinct().Pluck("type", &resTypes).Error)
	assert.Contains(t, resTypes, "link")
	assert.Contains(t, resTypes, "file")

	// 图片:演示图片资源落盘,且被内嵌进公开帖正文(信息流可见)
	assert.GreaterOrEqual(t, second.Images, 2, "应生成演示图片资源")
	var imageCount int64
	require.NoError(t, db.Table("resources").Where("mime_type LIKE ?", "image/%").Count(&imageCount).Error)
	assert.EqualValues(t, second.Images, imageCount, "图片资源数量应一致")
	var imagePostCount int64
	require.NoError(t, db.Table("documents").Where("content LIKE ?", "%![%](/api/resources/%/preview)%").Count(&imagePostCount).Error)
	assert.GreaterOrEqual(t, imagePostCount, int64(1), "至少 1 篇帖子内嵌图片")

	// 项目:至少 2 个;任务覆盖 4 种状态
	assert.GreaterOrEqual(t, second.Projects, 2)
	// 演示管理员须至少属于一个项目,保证 admin 登录后 /projects 有内容可演示
	var adminProjects int64
	require.NoError(t, db.Table("project_members").Where("user_id = ?", admin.ID).Count(&adminProjects).Error)
	assert.GreaterOrEqual(t, adminProjects, int64(1), "演示管理员应属于至少 1 个项目")
	var statuses []string
	require.NoError(t, db.Table("tasks").Distinct().Pluck("status", &statuses).Error)
	for _, want := range []string{"todo", "in_progress", "blocked", "done"} {
		assert.Contains(t, statuses, want, "任务状态覆盖")
	}

	// 经费:批次 + 流水
	assert.GreaterOrEqual(t, second.TurnoverBatches, 2)
	assert.GreaterOrEqual(t, second.Transactions, 1)

	// 种子后库非空 → 自动补种不再触发
	empty, err := demo.IsEmpty(ctx, db)
	require.NoError(t, err)
	assert.False(t, empty)
}
