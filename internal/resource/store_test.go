package resource_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/resource"
)

func TestLocalFileStore_QuarantineRestoreCleanup(t *testing.T) {
	baseDir := t.TempDir()
	store, err := resource.NewLocalFileStore(baseDir)
	require.NoError(t, err)

	path, size, err := store.Save(strings.NewReader("pdf-data"), "paper.pdf")
	require.NoError(t, err)
	assert.Positive(t, size)

	quarantinePath, err := store.Quarantine(path)
	require.NoError(t, err)
	assert.NotEmpty(t, quarantinePath)
	_, err = store.Open(path)
	assert.Error(t, err, "隔离期间原资源接口不可访问文件")

	require.NoError(t, store.Restore(path, quarantinePath))
	f, err := store.Open(path)
	require.NoError(t, err)
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	assert.Equal(t, "pdf-data", string(data))

	quarantinePath, err = store.Quarantine(path)
	require.NoError(t, err)
	require.NoError(t, store.DeleteQuarantined(quarantinePath))
	entries, err := os.ReadDir(filepath.Join(baseDir, ".trash"))
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestLocalFileStore_QuarantineMissingFileIsIdempotent(t *testing.T) {
	store, err := resource.NewLocalFileStore(t.TempDir())
	require.NoError(t, err)

	quarantinePath, err := store.Quarantine("uploads/missing.pdf")
	require.NoError(t, err)
	assert.Empty(t, quarantinePath)
}
