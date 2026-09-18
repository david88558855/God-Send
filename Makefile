# 神传 - Makefile
# 局域网文件传输工具

.PHONY: all build build-cli build-wails build-android clean dev run test

# 变量
APP_NAME := shenchuan
VERSION := 1.0.0
BUILD_DIR := build
GO := go
GOFLAGS := -trimpath -ldflags="-s -w -X main.version=$(VERSION)"

# 默认目标
all: build-cli

# 构建CLI版本
build-cli:
	@echo "构建CLI版本..."
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(APP_NAME).exe ./cmd/shenchuan

# 构建Wails桌面应用(Windows)
build-wails:
	@echo "构建Wails桌面应用..."
	wails build -clean -o $(APP_NAME).exe -ldflags "-s -w -X main.version=$(VERSION)"

# 构建Android AAR绑定
build-android:
	@echo "构建Android绑定..."
	$(GO) install golang.org/x/mobile/cmd/gomobile@latest
	gomobile init
	gomobile bind -target=android/arm64 -o $(BUILD_DIR)/$(APP_NAME).aar ./cmd/android

# 开发模式(Wails)
dev:
	wails dev

# 运行CLI
run:
	$(GO) run ./cmd/shenchuan

# 清理构建产物
clean:
	@echo "清理构建产物..."
	@if exist $(BUILD_DIR) rmdir /s /q $(BUILD_DIR)
	$(GO) clean

# 运行测试
test:
	$(GO) test ./... -v

# 下载依赖
deps:
	$(GO) mod download
	$(GO) mod tidy

# 格式化代码
fmt:
	$(GO) fmt ./...

# 代码检查
lint:
	golangci-lint run ./...

# 构建所有平台
build-all: build-cli build-wails build-android
	@echo "所有平台构建完成!"

# 显示帮助
help:
	@echo "神传 - 局域网文件传输工具"
	@echo ""
	@echo "可用目标:"
	@echo "  build-cli      构建CLI版本(Windows可执行文件)"
	@echo "  build-wails    构建Wails桌面应用(Windows)"
	@echo "  build-android  构建Android AAR绑定"
	@echo "  build-all      构建所有平台"
	@echo "  dev            启动Wails开发模式"
	@echo "  run            运行CLI版本"
	@echo "  clean          清理构建产物"
	@echo "  test           运行测试"
	@echo "  deps           下载依赖"
	@echo "  fmt            格式化代码"
	@echo "  lint           代码检查"
	@echo "  help           显示此帮助信息"