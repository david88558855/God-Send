package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"God-Send/godsend"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed frontend/dist
var frontendDist embed.FS

// 全局配置（供app.go使用）
var appConfig *godsend.Config

// addFirewallRule 添加Windows防火墙入站规则（允许指定端口的连接）
func addFirewallRule(name string, protocol string, port int) {
	cmd := exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
		"name="+name, "dir=in", "action=allow", "protocol="+protocol,
		fmt.Sprintf("localport=%d", port), "profile=any")
	// 隐藏子进程窗口，避免黑框闪现
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	if err := cmd.Run(); err != nil {
		log.Printf("[主程序] 添加防火墙规则失败(%s %s %d): %v，可能需要管理员权限", name, protocol, port, err)
	} else {
		log.Printf("[主程序] 已添加防火墙规则: %s (%s %d)", name, protocol, port)
	}
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("[主程序] God-Send 启动中...")

	// 加载配置
	cfg := godsend.LoadConfig()
	appConfig = cfg
	log.Printf("[主程序] 设备: %s (%s), QUIC端口: %d", cfg.GetDeviceName(), cfg.GetDeviceID(), cfg.GetPort())

	// 添加Windows防火墙规则，允许局域网设备连接
	addFirewallRule("GodSend-HTTP", "TCP", cfg.HTTPPort)
	addFirewallRule("GodSend-QUIC", "UDP", cfg.GetPort())
	addFirewallRule("GodSend-Discovery", "UDP", cfg.HTTPPort)

	// 确保文件保存目录存在
	savePath := cfg.GetSavePath()
	if err := os.MkdirAll(savePath, 0755); err != nil {
		log.Printf("[主程序] 创建保存目录失败: %v", err)
	}

	// 创建设备发现服务
	disc := godsend.NewDiscovery(cfg)

	// 创建网络服务
	net := godsend.NewNetwork(cfg, disc)

	// 创建API服务
	api := godsend.NewAPI(cfg, disc, net)

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
		log.Fatalf("[主程序] 设备发现启动失败: %v", err)
	}

	// 启动QUIC网络
	if err := net.Start(); err != nil {
		log.Fatalf("[主程序] QUIC网络启动失败: %v", err)
	}

	// 启动HTTP API（为浏览器客户端提供服务）
	if err := api.Start(); err != nil {
		log.Fatalf("[主程序] API服务启动失败: %v", err)
	}

	log.Printf("[主程序] HTTP服务已启动，局域网设备可通过浏览器访问 http://<本机IP>:%d", cfg.HTTPPort)

	// 获取HTTP handler用于Wails AssetServer
	httpHandler := api.GetHandler()

	// 创建Wails应用
	app := NewApp()

	// 在单独的goroutine中等待退出信号
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("[主程序] 收到退出信号，正在关闭...")
	}()

	// 运行Wails应用
	err := wails.Run(&options.App{
		Title:     "God-Send",
		Width:     1200,
		Height:    800,
		MinWidth:  800,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Handler: httpHandler,
		},
		OnStartup:  app.startup,
		OnShutdown: func(ctx context.Context) {
			log.Println("[主程序] 正在关闭...")
			api.Stop()
			net.Stop()
			disc.Stop()
			log.Println("[主程序] 已关闭")
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		log.Fatalf("[主程序] Wails启动失败: %v", err)
	}
}