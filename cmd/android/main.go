//go:build android

package main

import (
	"fmt"
	"log"

	"github.com/shenchuan/shenchuan/internal/config"
	"github.com/shenchuan/shenchuan/internal/discovery"
	"github.com/shenchuan/shenchuan/internal/server"
	"github.com/shenchuan/shenchuan/internal/transfer"
)

// ShenchuanApp Android绑定应用
type ShenchuanApp struct {
	config    *config.Config
	discovery *discovery.Discovery
	server    *server.Server
	transfer  *transfer.Manager
	running   bool
}

// NewShenchuanApp 创建应用实例(导出给Android调用)
func NewShenchuanApp() *ShenchuanApp {
	return &ShenchuanApp{}
}

// Start 启动服务(导出给Android调用)
func (a *ShenchuanApp) Start() error {
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
		log.Printf("警告: 设备发现启动失败: %v", err)
	}

	// 启动服务器
	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("服务器启动失败: %v", err)
		}
	}()

	a.running = true
	log.Println("神传Android服务已启动")
	return nil
}

// Stop 停止服务(导出给Android调用)
func (a *ShenchuanApp) Stop() {
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
	log.Println("神传Android服务已停止")
}

// GetServerURL 获取服务器地址(导出给Android WebView调用)
func (a *ShenchuanApp) GetServerURL() string {
	if !a.running || a.config == nil {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d", a.config.Port)
}

// GetDeviceInfo 获取设备信息(导出给Android调用)
func (a *ShenchuanApp) GetDeviceInfo() map[string]interface{} {
	if a.config == nil {
		return nil
	}
	return map[string]interface{}{
		"device_name": a.config.DeviceName,
		"device_id":   a.discovery.GetDeviceID(),
		"port":        a.config.Port,
		"platform":    "android",
	}
}

func main() {
	// Android入口由gomobile绑定管理
	// 此main函数仅用于编译检查
	app := NewShenchuanApp()
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}

	// 阻塞等待
	select {}
}