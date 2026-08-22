# LabNexus 开发工具链
# 规范依据:docs/standards.md §8
# 使用:make <target>;提交前必跑 make check

.PHONY: up down run build test lint check tidy

## 容器
up:
	docker compose up -d

down:
	docker compose down

## 开发
run:
	go run ./cmd/server

build:
	go build ./...

tidy:
	go mod tidy

## 质量
test:
	go test ./... -cover

lint:
	golangci-lint run ./...

## 全量检查(提交前必跑):vet + fmt + test + lint + build
check:
	./scripts/check.sh
