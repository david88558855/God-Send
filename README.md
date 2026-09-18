# God-Send

局域网文件传输工具 —— 在同一网络中的设备之间即时发送文件和消息，无需互联网连接。

## 功能特性

- **设备自动发现** — 基于 mDNS 协议自动发现局域网内的 God-Send 设备
- **即时消息** — 设备间实时文字聊天，WebSocket 长连接通信
- **文件传输** — 拖拽或选择文件发送，支持大文件分块传输（最大 10GB）
- **传输管理** — 实时进度显示，传输列表面板
- **Material 3 风格界面** — 现代化 UI，淡灰色浅色主题
- **跨平台** — 支持 Windows 桌面应用，可编译 Android AAR 绑定

## 技术栈

| 层级 | 技术 |
|------|------|
| 桌面框架 | [Wails v2](https://wails.io/)（Go + WebView） |
| 前端 | 原生 HTML/CSS/JS，Material 3 Expressive 设计 |
| 设备发现 | mDNS（`_godsend._tcp`） |
| 实时通信 | WebSocket |
| 文件传输 | HTTP 分块上传/下载 |
| Android 绑定 | gomobile |

## 项目结构

```
├── main.go                 # Wails 应用入口
├── cmd/
│   ├── wails/main.go       # Wails 构建入口（含 embed 指令）
│   └── android/main.go     # Android AAR 绑定（库包）
├── internal/
│   ├── config/             # 应用配置（端口、目录、设备名等）
│   ├── discovery/          # mDNS 设备发现
│   ├── network/            # 网络工具
│   ├── protocol/           # 消息协议定义
│   ├── server/             # HTTP + WebSocket 服务
│   └── transfer/           # 文件传输管理
├── frontend/
│   ├── dist/index.html     # 前端单页应用
│   └── wailsjs/            # Wails JS 绑定
├── build/
│   ├── appicon.png         # 应用图标
│   └── windows/            # Windows 构建资源（图标、清单）
├── .github/workflows/      # GitHub Actions CI
├── wails.json              # Wails 配置
├── Makefile                # 构建命令
├── go.mod / go.sum         # Go 依赖
└── .gitignore
```

## 快速开始

### 环境要求

- Go 1.21+
- [Wails CLI v2](https://wails.io/docs/gettingstarted/installation)
- Windows: WebView2 Runtime（Windows 10/11 已内置）

### 构建

```bash
# 安装依赖
go mod download

# 构建 Windows 桌面应用
wails build -clean -s -skipbindings

# 输出: build/bin/God-Send.exe
```

### 开发模式

```bash
wails dev
```

### 构建 Android 绑定

```bash
# 需要安装 gomobile 和 Android NDK
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init
gomobile bind -target=android/arm64 -o build/God-Send.aar ./cmd/android
```

## 使用方法

1. 启动 God-Send，应用自动在局域网中广播设备信息
2. 其他设备也启动 God-Send 后，设备列表会自动显示在线设备
3. 点击设备开始聊天，或直接拖拽文件到窗口发送
4. 传输面板可查看所有文件传输进度

## 配置

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| 端口 | 5780 | HTTP/WebSocket 服务端口 |
| 设备名称 | 主机名 | 在局域网中显示的名称 |
| 下载目录 | ~/Downloads/God-Send | 接收文件的保存路径 |
| 最大文件大小 | 10 GB | 单次传输文件大小上限 |

## 许可证

MIT License