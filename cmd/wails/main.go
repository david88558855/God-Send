//go:build ignore

package main

import (
	"context"
	"embed"
	"log"

	"github.com/God-Send/God-Send/internal/config"
	"github.com/God-Send/God-Send/internal/discovery"
	"github.com/God-Send/God-Send/internal/protocol"
	"github.com/God-Send/God-Send/internal/server"
	"github.com/God-Send/God-Send/internal/transfer"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:../../frontend/dist
var assets embed.FS

func main() {
	// 创建配置
	cfg := config.DefaultConfig()

	// 确保目录存在
	if err := cfg.EnsureDirs(); err != nil {
		log.Fatalf("初始化目录失败: %v", err)
	}

	// 创建发现服务
	disc := discovery.New(cfg)

	// 创建传输管理器
	mgr := transfer.NewManager(cfg)

	// 创建并启动HTTP服务器
	srv := server.New(cfg, disc, mgr)

	// 设置设备发现回调
	disc.SetCallbacks(
		func(peer *protocol.DeviceInfo) {
			log.Printf("[发现] 设备上线: %s", peer.Name)
		},
		func(id string) {
			log.Printf("[发现] 设备离线: %s", id)
		},
	)

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

	// 创建Wails应用
	app := &App{
		config:    cfg,
		discovery: disc,
		server:    srv,
	}

	// 启动Wails
	if err := wails.Run(&options.App{
		Title:     "God-Send - LAN File Transfer",
		Width:     1024,
		Height:    768,
		MinWidth:  768,
		MinHeight: 540,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 1},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	}); err != nil {
		log.Fatalf("Wails启动失败: %v", err)
	}
}

// App Wails应用绑定
type App struct {
	ctx       context.Context
	config    *config.Config
	discovery *discovery.Discovery
	server    *server.Server
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	log.Println("God-Send desktop app started")
}

func (a *App) shutdown(ctx context.Context) {
	log.Println("God-Send desktop app shutting down...")
	a.server.Stop()
	a.discovery.Stop()
}

// GetDeviceInfo 返回设备信息(供前端调用)
func (a *App) GetDeviceInfo() map[string]interface{} {
	return map[string]interface{}{
		"device_name": a.config.DeviceName,
		"device_id":   a.discovery.GetDeviceID(),
		"port":        a.config.Port,
		"platform":    "windows",
	}
}