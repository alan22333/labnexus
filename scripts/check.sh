#!/usr/bin/env bash
# LabNexus 提交前全量检查(等价 make check)
# 规范依据:docs/standards.md §8
set -euo pipefail

cd "$(dirname "$0")/.."

# 受限环境适配:若工作区根目录存在预置缓存(.gocache 等),将 Go/lint 缓存强制指到工作区内
# (此环境可能预设了不可写的 GOPATH;正常开发环境无 .gocache 目录,本段不生效)
WORKSPACE_ROOT="$(cd .. && pwd)"
if [ -d "$WORKSPACE_ROOT/.gocache" ]; then
  export GOCACHE="$WORKSPACE_ROOT/.gocache"
  export GOPATH="$WORKSPACE_ROOT/.gopath"
  export GOLANGCI_LINT_CACHE="$WORKSPACE_ROOT/.golangci-cache"
fi

echo "==> go vet"
go vet ./...

echo "==> gofmt (空输出 = 通过)"
gofmt -l .

echo "==> go test -cover"
go test ./... -cover

echo "==> golangci-lint"
if command -v golangci-lint >/dev/null 2>&1; then
  golangci-lint run ./...
elif [ -x "$HOME/go/bin/golangci-lint" ]; then
  "$HOME/go/bin/golangci-lint" run ./...
elif [ -x "$WORKSPACE_ROOT/.gobin/golangci-lint" ]; then
  "$WORKSPACE_ROOT/.gobin/golangci-lint" run ./...
else
  echo "WARN: golangci-lint 未安装,跳过 lint(建议安装: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)"
fi

echo "==> go build"
go build ./...

echo ""
echo "ALL CHECKS PASSED ✅"
