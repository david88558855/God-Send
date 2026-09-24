// Package mobile provides Go bindings for Android via gomobile.
// It exposes the God-Send LAN chat core as a library that can be
// embedded in an Android application.
package mobile

import (
	"fmt"
	"log"
	"os"

	"God-Send/godsend"
)

// App represents the God-Send mobile application.
// gomobile binds exported struct types and their exported methods.
type App struct {
	config    *godsend.Config
	discovery *godsend.Discovery
	network   *godsend.Network
	api       *godsend.API
}

// NewApp creates a new God-Send mobile app instance.
func NewApp() *App {
	return &App{}
}

// Start initializes and starts all God-Send services.
// Returns the HTTP port for the WebView to connect to.
func (a *App) Start() int {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("[Mobile] God-Send 启动中...")

	// 加载配置
	cfg := godsend.LoadConfig()
	a.config = cfg
	log.Printf("[Mobile] 设备: %s (%s), QUIC端口: %d", cfg.GetDeviceName(), cfg.GetDeviceID(), cfg.GetPort())

	// 确保文件保存目录存在
	savePath := cfg.GetSavePath()
	if err := os.MkdirAll(savePath, 0755); err != nil {
		log.Printf("[Mobile] 创建保存目录失败: %v", err)
	}

	// 创建设备发现服务
	disc := godsend.NewDiscovery(cfg)
	a.discovery = disc

	// 创建网络服务
	net := godsend.NewNetwork(cfg, disc)
	a.network = net

	// 创建API服务
	api := godsend.NewAPI(cfg, disc, net)
	a.api = api

	// 设置回调：网络消息 -> API处理
	net.SetOnMessage(func(msg *godsend.Message) {
		api.OnIncomingMessage(msg)
	})

	// 设置回调：文件传输完成 -> API处理
	net.SetOnFileComplete(func(fileID string, pf *godsend.PendingFile, finalPath string) {
		api.OnFileComplete(fileID, pf, finalPath)
	})

	// 设置回调：文件传输进度 -> API广播
	net.SetOnFileProgress(func(fileID string, received int64, total int64) {
		api.OnFileProgress(fileID, received, total)
	})

	// 设置回调：设备变化 -> 通知前端
	disc.SetOnDeviceChange(func() {
		api.OnDevicesChanged()
	})

	// 启动设备发现
	if err := disc.Start(); err != nil {
		log.Fatalf("[Mobile] 设备发现启动失败: %v", err)
	}

	// 启动QUIC网络
	if err := net.Start(); err != nil {
		log.Fatalf("[Mobile] QUIC网络启动失败: %v", err)
	}

	// 启动HTTP API
	if err := api.Start(); err != nil {
		log.Fatalf("[Mobile] API服务启动失败: %v", err)
	}

	log.Printf("[Mobile] HTTP服务已启动，端口: %d", cfg.HTTPPort)
	return cfg.HTTPPort
}

// Stop shuts down all God-Send services.
func (a *App) Stop() {
	if a.api != nil {
		a.api.Stop()
	}
	if a.network != nil {
		a.network.Stop()
	}
	if a.discovery != nil {
		a.discovery.Stop()
	}
	log.Println("[Mobile] 已关闭")
}

// GetHTTPPort returns the HTTP port for WebView connection.
func (a *App) GetHTTPPort() int {
	if a.config != nil {
		return a.config.GetHTTPPort()
	}
	return 53334
}

// GetDeviceName returns the device name.
func (a *App) GetDeviceName() string {
	if a.config != nil {
		return a.config.GetDeviceName()
	}
	return "未知设备"
}

// GetDeviceID returns the device ID.
func (a *App) GetDeviceID() string {
	if a.config != nil {
		return a.config.GetDeviceID()
	}
	return ""
}

// GetURL returns the URL for the WebView to load.
func (a *App) GetURL() string {
	port := a.GetHTTPPort()
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}