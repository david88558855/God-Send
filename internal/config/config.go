package config

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// AppName 应用名称
	AppName = "神传"
	// AppID 应用标识
	AppID = "com.shenchuan.app"
	// Version 版本号
	Version = "1.0.0"
	// DefaultPort 默认HTTP服务端口
	DefaultPort = 5780
	// DefaultWebSocketPath WebSocket路径
	DefaultWebSocketPath = "/ws"
	// DefaultAPIPath API路径前缀
	DefaultAPIPath = "/api"
	// MDNSServiceName mDNS服务名称
	MDNSServiceName = "_shenchuan._tcp"
	// MDNSDomain mDNS域
	MDNSDomain = "local."
	// MaxFileSize 最大文件大小 (10GB)
	MaxFileSize int64 = 10 * 1024 * 1024 * 1024
	// ChunkSize 文件分块大小 (1MB)
	ChunkSize int64 = 1024 * 1024
	// TransferDir 传输目录名
	TransferDir = "transfers"
	// ThumbnailDir 缩略图目录名
	ThumbnailDir = "thumbnails"
)

// Config 应用配置
type Config struct {
	// DeviceName 设备名称
	DeviceName string
	// Port HTTP服务端口
	Port int
	// DataDir 数据目录
	DataDir string
	// DownloadDir 下载目录
	DownloadDir string
	// AvatarColor 头像颜色
	AvatarColor string
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	dataDir := filepath.Join(homeDir, ".shenchuan")
	downloadDir := filepath.Join(homeDir, "Downloads", "ShenChuan")

	return &Config{
		DeviceName:  getDeviceName(),
		Port:        DefaultPort,
		DataDir:     dataDir,
		DownloadDir: downloadDir,
		AvatarColor: randomColor(),
	}
}

// EnsureDirs 确保必要目录存在
func (c *Config) EnsureDirs() error {
	dirs := []string{
		c.DataDir,
		c.DownloadDir,
		filepath.Join(c.DataDir, TransferDir),
		filepath.Join(c.DataDir, ThumbnailDir),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建目录失败 %s: %w", dir, err)
		}
	}
	return nil
}

// getDeviceName 获取设备名称
func getDeviceName() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "未知设备"
	}
	return hostname
}

// randomColor 生成随机颜色
func randomColor() string {
	b := make([]byte, 3)
	rand.Read(b)
	return fmt.Sprintf("#%02x%02x%02x", b[0], b[1], b[2])
}