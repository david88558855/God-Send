package godsend

import (
	"fmt"
	"log"

	"github.com/God-Send/God-Send/internal/config"
	"github.com/God-Send/God-Send/internal/discovery"
	"github.com/God-Send/God-Send/internal/server"
	"github.com/God-Send/God-Send/internal/transfer"
)

// DeviceInfo 设备信息(导出给Android调用)
type DeviceInfo struct {
	DeviceName string
	DeviceID   string
	Port       int
	Platform   string
}

// GodSendApp Android绑定应用
type GodSendApp struct {
	config    *config.Config
	discovery *discovery.Discovery
	server    *server.Server
	transfer  *transfer.Manager
	running   bool
}

// NewGodSendApp 创建应用实例(导出给Android调用)
func NewGodSendApp() *GodSendApp {
	return &GodSendApp{}
}

// Start 启动服务(导出给Android调用)
func (a *GodSendApp) Start() error {
	if a.running {
		return nil
	}

	cfg := config.DefaultConfig()
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	a.config = cfg

	// 创建发现服务
	disc := discovery.New(cfg)
	a.discovery = disc

	// 创建传输管理器
	mgr := transfer.NewManager(cfg)
	a.transfer = mgr

	// 创建并启动HTTP服务器
	srv := server.New(cfg, disc, mgr)
	a.server = srv

	// 启动设备发现
	if err := disc.Start(); err != nil {
		log.Printf("Warning: device discovery failed: %v", err)
	}

	// 启动服务器
	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("Server start failed: %v", err)
		}
	}()

	a.running = true
	log.Println("God-Send Android service started")
	return nil
}

// Stop 停止服务(导出给Android调用)
func (a *GodSendApp) Stop() {
	if !a.running {
		return
	}

	if a.server != nil {
		a.server.Stop()
	}
	if a.discovery != nil {
		a.discovery.Stop()
	}
	a.running = false
	log.Println("God-Send Android service stopped")
}

// GetServerURL 获取服务器地址(导出给Android WebView调用)
func (a *GodSendApp) GetServerURL() string {
	if !a.running || a.config == nil {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d", a.config.Port)
}

// GetDeviceInfo 获取设备信息(导出给Android调用)
func (a *GodSendApp) GetDeviceInfo() *DeviceInfo {
	if a.config == nil {
		return nil
	}
	return &DeviceInfo{
		DeviceName: a.config.DeviceName,
		DeviceID:   a.discovery.GetDeviceID(),
		Port:       a.config.Port,
		Platform:   "android",
	}
}