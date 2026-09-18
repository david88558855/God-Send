package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
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

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	cfg := config.DefaultConfig()

	if err := cfg.EnsureDirs(); err != nil {
		log.Fatalf("初始化目录失败: %v", err)
	}

	disc := discovery.New(cfg)
	mgr := transfer.NewManager(cfg)
	srv := server.New(cfg, disc, mgr)

	disc.SetCallbacks(
		func(peer *protocol.DeviceInfo) {
			log.Printf("[发现] 设备上线: %s", peer.Name)
		},
		func(id string) {
			log.Printf("[发现] 设备离线: %s", id)
		},
	)

	if err := disc.Start(); err != nil {
		log.Printf("警告: 设备发现启动失败: %v", err)
	}

	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("服务器启动失败: %v", err)
		}
	}()

	app := &App{
		config:    cfg,
		discovery: disc,
		server:    srv,
		transfer:  mgr,
	}

	if err := wails.Run(&options.App{
		Title:     "God-Send",
		Width:     1024,
		Height:    768,
		MinWidth:  768,
		MinHeight: 540,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 28, G: 27, B: 31, A: 255},
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
	transfer  *transfer.Manager
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

// GetDeviceInfo 返回设备信息
func (a *App) GetDeviceInfo() map[string]interface{} {
	return map[string]interface{}{
		"device_name": a.config.DeviceName,
		"device_id":   a.discovery.GetDeviceID(),
		"port":        a.config.Port,
		"platform":    "windows",
		"color":       a.config.AvatarColor,
		"download_dir": a.config.DownloadDir,
	}
}

// GetServerURL 返回本地服务器URL
func (a *App) GetServerURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", a.config.Port)
}

// GetPeers 返回已发现的设备列表
func (a *App) GetPeers() string {
	peers := a.discovery.GetPeers()
	data, _ := json.Marshal(peers)
	return string(data)
}

// GetTransfers 返回传输列表
func (a *App) GetTransfers() string {
	transfers := a.transfer.GetTransfers()
	data, _ := json.Marshal(transfers)
	return string(data)
}

// SetDeviceName 设置设备名称
func (a *App) SetDeviceName(name string) {
	a.config.DeviceName = name
}

// GetConfig 返回配置信息
func (a *App) GetConfig() string {
	data, _ := json.Marshal(map[string]interface{}{
		"device_name":  a.config.DeviceName,
		"port":         a.config.Port,
		"download_dir": a.config.DownloadDir,
		"color":        a.config.AvatarColor,
	})
	return string(data)
}