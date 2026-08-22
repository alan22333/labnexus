# LabNexus 开发工具链
# 规范依据:docs/standards.md §8
# 使用:make <target>;提交前必跑 make check

.PHONY: up down run build test lint check tidy

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

## 质量
test:
	go test ./... -cover

# 集成测试(真实 Postgres+Redis,需先 make up;环境未就绪自动跳过)
test-integration:
	go test -tags integration -v ./test/integration/...

lint:
	golangci-lint run ./...

## 全量检查(提交前必跑):vet + fmt + test + lint + build
check:
	./scripts/check.sh
