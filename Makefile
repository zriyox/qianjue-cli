MODULE      := github.com/zriyox/qianjue-cli
VERSION     ?= 1.0.0-dev
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE  := $(shell date -u +%Y-%m-%d)
LDFLAGS     := -X $(MODULE)/internal/buildinfo.Version=$(VERSION) \
               -X $(MODULE)/internal/buildinfo.Commit=$(COMMIT) \
               -X $(MODULE)/internal/buildinfo.BuildDate=$(BUILD_DATE)
BIN_DIR     := bin
PREFIX      ?= /usr/local

.PHONY: build test lint fmt install install-skill clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/qianjue ./cmd/qianjue

test:
	go test ./...

lint:
	@fmt_out="$$(gofmt -l .)"; \
	if [ -n "$$fmt_out" ]; then \
		echo "gofmt needed on:"; echo "$$fmt_out"; exit 1; \
	fi
	go vet ./...

fmt:
	gofmt -w .

install: build
	install -m 0755 $(BIN_DIR)/qianjue $(PREFIX)/bin/qianjue

# 可选便捷目标：只往已存在的 skills 目录写；一个都没有会失败并提示用 --dir。
# 故意不挂进 `install`——装 CLI 不该顺手改用户的助手配置。
# Windows 无 make，直接运行 `qianjue skill install` 效果相同。
install-skill: build
	./$(BIN_DIR)/qianjue skill install

clean:
	rm -rf $(BIN_DIR)
