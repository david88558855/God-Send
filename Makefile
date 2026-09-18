# God-Send - Makefile
# LAN File Transfer Tool

.PHONY: all build build-wails build-android clean dev test

# 变量
APP_NAME := God-Send
VERSION := 1.0.0
BUILD_DIR := build
GO := go
GOFLAGS := -trimpath -ldflags="-s -w -X main.version=$(VERSION)"

# 默认目标
all: build-wails

# 构建Wails桌面应用(Windows便携版)
build:
	@echo "Building God-Send desktop app..."
	wails build -clean -o $(APP_NAME).exe -ldflags "-s -w -X main.version=$(VERSION)"

# 构建Wails桌面应用(Windows便携版)
build-wails:
	@echo "Building God-Send desktop app..."
	wails build -clean -o $(APP_NAME).exe -ldflags "-s -w -X main.version=$(VERSION)"

# 构建Android AAR绑定
build-android:
	@echo "Building Android binding..."
	$(GO) install golang.org/x/mobile/cmd/gomobile@latest
	gomobile init
	gomobile bind -target=android/arm64 -o $(BUILD_DIR)/$(APP_NAME).aar ./cmd/android

# 开发模式(Wails)
dev:
	wails dev

# 清理构建产物
clean:
	@echo "Cleaning build artifacts..."
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
build-all: build-wails build-android
	@echo "All platforms built!"

# 显示帮助
help:
	@echo "God-Send - LAN File Transfer Tool"
	@echo ""
	@echo "Available targets:"
	@echo "  build           Build Wails desktop app (Windows portable)"
	@echo "  build-wails     Build Wails desktop app (Windows portable)"
	@echo "  build-android   Build Android AAR binding"
	@echo "  build-all       Build all platforms"
	@echo "  dev             Start Wails dev mode"
	@echo "  clean           Clean build artifacts"
	@echo "  test            Run tests"
	@echo "  deps            Download dependencies"
	@echo "  fmt             Format code"
	@echo "  lint            Code linting"
	@echo "  help            Show this help"