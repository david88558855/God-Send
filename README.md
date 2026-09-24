# God-Send

局域网聊天软件 — 无中心、自动发现、即时通讯与文件传输。

## 功能

- **设备自动发现**：UDP 广播 + mDNS 双重发现机制，局域网内设备自动上线
- **即时聊天**：文本消息实时收发，在线/离线状态一览
- **文件传输**：支持任意文件和图片传输，实时进度显示，自动保存到下载目录
- **网页版**：内置 HTTP/WebSocket 服务，浏览器即可访问（可关闭）
- **桌面端**：基于 Wails 框架，Windows 原生窗口

## 架构

```
┌─────────────┐    QUIC :53333    ┌─────────────┐
│  God-Send A  │◄────────────────►│  God-Send B  │
│  (Desktop)   │                  │  (Desktop)   │
└──────┬───────┘                  └──────────────┘
       │ HTTP/WS :53334
       │
  ┌────▼────┐
  │ Browser │
  │ (Web)   │
  └─────────┘
```

| 协议 | 端口 | 用途 |
|------|------|------|
| QUIC | 53333 | 设备间加密通信 |
| UDP 广播 | 53334 | 设备发现 |
| HTTP/WS | 53334 | 网页版访问（可关闭） |

## 快速开始

### 前置要求

- Go 1.27+
- CGO (需安装 GCC，Windows 推荐 [MinGW-w64](https://www.mingw-w64.org/) 或 [TDM-GCC](https://jmeubank.github.io/tdm-gcc/))
- [Wails CLI](https://wails.io/docs/gettingstarted/installation) v2（桌面版编译）

### 编译桌面版

```bash
cd lan-chat
wails build
# 输出: build/bin/God-Send.exe
```

### 编译 Android 版

使用 [makeworld/gomobile-android](https://github.com/makeworld/gomobile-android) Docker 镜像：

```bash
cd lan-chat
docker run --rm -v "$(pwd):/work" -w /work \
  makeworld/gomobile-android bind -target android -o god-send.aar ./...
```

## 使用

1. 启动 God-Send，自动发现局域网内其他设备
2. 点击设备名称开始聊天
3. 支持发送文本、文件和图片
4. 设置中可开启/关闭网页版访问
5. 关闭网页版后，浏览器客户端将断开连接

## 配置

首次运行自动生成配置文件 `god-send-config.json`：

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| deviceName | 主机名 | 设备显示名称 |
| quicPort | 53333 | QUIC 通信端口 |
| httpPort | 53334 | HTTP/WS 服务端口 |
| webMode | true | 是否开启网页版 |
| savePath | ~/Downloads | 文件保存路径 |

## 技术栈

- [Go](https://go.dev/) 1.27 — 后端语言
- [quic-go](https://github.com/quic-go/quic-go) — QUIC 协议通信
- [gorilla/websocket](https://github.com/gorilla/websocket) — WebSocket 服务
- [hashicorp/mdns](https://github.com/hashicorp/mdns) — mDNS 设备发现
- [Wails](https://wails.io/) v2 — 桌面 GUI 框架

## 许可证

MIT License