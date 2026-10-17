# LabNexus 开发工具链
# 规范依据:docs/standards.md §8
# 使用:make <target>;提交前必跑 make check

.PHONY: up stop down run build test lint check tidy seed-admin seed-demo demo \
	migrate-up migrate-down migrate-status migrate-baseline backup restore test-integration

## 容器
up:
	docker compose up -d

# 暂停容器(保留容器与数据,快速重启:docker compose start)
stop:
	docker compose stop

# 停止并移除容器(数据卷保留,下次 up 数据仍在)
down:
	docker compose down

## 开发
run:
	go run ./cmd/server

build:
	go build ./...

tidy:
	go mod tidy

## 数据库迁移(goose,见 docs/specs/database-migration-backup.md)
migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down

migrate-status:
	go run ./cmd/migrate status

# 已有 AutoMigrate 库的一次性基线标记(保留数据);空库等价 migrate-up
migrate-baseline:
	go run ./cmd/migrate baseline

## 备份恢复(PostgreSQL custom dump + 上传目录 + 校验清单)
backup:
	bash scripts/backup.sh

# 用法:make restore FILE=backups/xxx.tar.gz CONFIRM=yes
restore:
	bash scripts/restore.sh

## 质量
test:
	go test ./... -cover

# 种子:创建固定测试管理员 test_admin / Test@123456(清库后可随时恢复)
seed-admin:
	bash scripts/seed-test-admin.sh

# 演示数据:清空业务表并写入高保真数据(覆盖社区/资源/项目/经费)。可反复执行,结果一致。
# 账号:test_admin / Test@123456;supervisor / Demo@123456;student1~4 / Demo@123456
seed-demo:
	go run ./cmd/seed-demo

# 一键体验:启动容器 + 重建演示数据 + 启动后端(开发环境空库也会自动补种)
demo: up seed-demo
	go run ./cmd/server

# 集成测试(真实 Postgres+Redis,需先 make up;环境未就绪自动跳过)
test-integration:
	go test -tags integration -v ./test/integration/...

lint:
	golangci-lint run ./...

## 全量检查(提交前必跑):vet + fmt + test + lint + build
check:
	./scripts/check.sh
