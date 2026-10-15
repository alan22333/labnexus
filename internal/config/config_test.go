package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/config"
)

func TestLoad_TestAuthzBypassDefaultsOff(t *testing.T) {
	t.Setenv("AUTHZ_TEST_BYPASS", "")
	cfg := config.Load()
	assert.False(t, cfg.AuthzTestBypass)
}

func TestLoad_TestAuthzBypassExplicitlyEnabled(t *testing.T) {
	t.Setenv("AUTHZ_TEST_BYPASS", "true")
	cfg := config.Load()
	assert.True(t, cfg.AuthzTestBypass)
}

func TestLoad_MigrationsDirDefault(t *testing.T) {
	t.Setenv("MIGRATIONS_DIR", "")
	cfg := config.Load()
	assert.Equal(t, "migrations", cfg.MigrationsDir)
}

func TestValidate_RejectsProductionTestBypass(t *testing.T) {
	cfg := &config.Config{AppEnv: "production", AuthzTestBypass: true}
	require.Error(t, cfg.Validate())
}

func TestValidate_AllowsDevelopmentTestBypass(t *testing.T) {
	cfg := &config.Config{AppEnv: "development", AuthzTestBypass: true}
	require.NoError(t, cfg.Validate())
}
