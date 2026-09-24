package godsend

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Config 应用配置
type Config struct {
	mu         sync.RWMutex
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	Port       int    `json:"port"`
	HTTPPort   int    `json:"httpPort"`
	SavePath   string `json:"savePath"` // 文件保存路径
	WebMode    bool   `json:"webMode"`  // 网页版模式（允许浏览器访问）
	configPath string
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	savePath := filepath.Join(homeDir, "Downloads")
	cfg := &Config{
		DeviceID:   generateDeviceID(),
		DeviceName: getHostname(),
		Port:       53333,
		HTTPPort:   53334,
		SavePath:   savePath,
		WebMode:    true,
	}
	cfg.configPath = cfg.getConfigPath()
	return cfg
}

// LoadConfig 加载配置
func LoadConfig() *Config {
	cfg := DefaultConfig()
	data, err := os.ReadFile(cfg.configPath)
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return DefaultConfig()
	}
	cfg.configPath = cfg.getConfigPath()
	return cfg
}

// Save 保存配置
func (c *Config) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	dir := filepath.Dir(c.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.configPath, data, 0644)
}

// Update 更新配置
func (c *Config) Update(deviceName string, port int, httpPort int, savePath string, webMode bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if deviceName != "" {
		c.DeviceName = deviceName
	}
	if port > 0 && port <= 65535 {
		c.Port = port
	}
	if httpPort > 0 && httpPort <= 65535 {
		c.HTTPPort = httpPort
	}
	if savePath != "" {
		c.SavePath = savePath
	}
	c.WebMode = webMode
	return c.Save()
}

// GetHTTPPort 获取HTTP端口
func (c *Config) GetHTTPPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.HTTPPort
}

// GetWebMode 获取网页版模式
func (c *Config) GetWebMode() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.WebMode
}

// GetDeviceID 获取设备ID
func (c *Config) GetDeviceID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.DeviceID
}

// GetDeviceName 获取设备名
func (c *Config) GetDeviceName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.DeviceName
}

// GetPort 获取端口
func (c *Config) GetPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Port
}

// GetSavePath 获取保存路径
func (c *Config) GetSavePath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.SavePath
}

func (c *Config) getConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, "God-Send", "config.json")
}

func generateDeviceID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x", b[0:2], b[2:4], b[4:6], b[6:8])
}

func getHostname() string {
	name, err := os.Hostname()
	if err != nil {
		return "未知设备"
	}
	return name
}