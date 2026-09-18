package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/shenchuan/shenchuan/internal/config"
	"github.com/shenchuan/shenchuan/internal/discovery"
	"github.com/shenchuan/shenchuan/internal/network"
	"github.com/shenchuan/shenchuan/internal/protocol"
	"github.com/shenchuan/shenchuan/internal/server"
	"github.com/shenchuan/shenchuan/internal/transfer"
)

func main() {
	// 命令行参数
	port := flag.Int("port", config.DefaultPort, "服务端口")
	name := flag.String("name", "", "设备名称")
	dir := flag.String("dir", "", "下载目录")
	showIP := flag.Bool("ip", false, "显示本机IP地址")
	flag.Parse()

	// 显示IP模式
	if *showIP {
		ips, err := network.GetLocalIPs()
		if err != nil {
			fmt.Fprintf(os.Stderr, "获取IP地址失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("本机局域网IP地址:")
		for _, ip := range ips {
			fmt.Printf("  - %s\n", ip)
		}
		return
	}

	// 加载配置
	cfg := config.DefaultConfig()
	if *name != "" {
		cfg.DeviceName = *name
	}
	if *port != config.DefaultPort {
		cfg.Port = *port
	}
	if *dir != "" {
		cfg.DownloadDir = *dir
	}

	// 确保目录存在
	if err := cfg.EnsureDirs(); err != nil {
		log.Fatalf("初始化目录失败: %v", err)
	}

	// 打印启动信息
	printBanner(cfg)

	// 创建服务组件
	disc := discovery.New(cfg)
	mgr := transfer.NewManager(cfg)
	srv := server.New(cfg, disc, mgr)

	// 设置设备发现回调
	disc.SetCallbacks(
		func(peer *protocol.DeviceInfo) {
			log.Printf("[发现] 设备上线: %s (%s:%d)", peer.Name, peer.IPAddress, peer.Port)
		},
		func(id string) {
			log.Printf("[发现] 设备离线: %s", id)
		},
	)

	// 启动设备发现
	if err := disc.Start(); err != nil {
		log.Printf("警告: 设备发现启动失败: %v", err)
		log.Println("提示: 仍然可以通过IP地址手动连接")
	}

	// 启动服务器
	if err := srv.Start(); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}

	// 获取本机IP
	ip, err := network.GetPrimaryIP()
	if err != nil {
		ip = "127.0.0.1"
	}

	fmt.Println()
	fmt.Printf("  神传正在运行！\n")
	fmt.Printf("  局域网访问地址: http://%s:%d\n", ip, cfg.Port)
	fmt.Printf("  本机访问地址: http://127.0.0.1:%d\n", cfg.Port)
	fmt.Println()
	fmt.Println("  按 Ctrl+C 退出")
	fmt.Println()

	// 等待退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println()
	log.Println("正在关闭神传...")

	// 优雅关闭
	srv.Stop()
	disc.Stop()

	log.Println("神传已关闭")
}

func printBanner(cfg *config.Config) {
	fmt.Println()
	fmt.Println("  ╔══════════════════════════════════╗")
	fmt.Println("  ║          神  传                   ║")
	fmt.Println("  ║    局域网文件传输 & 聊天工具      ║")
	fmt.Println("  ╚══════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("  版本:     %s\n", config.Version)
	fmt.Printf("  设备:     %s\n", cfg.DeviceName)
	fmt.Printf("  平台:     %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  端口:     %d\n", cfg.Port)
	fmt.Printf("  下载目录: %s\n", cfg.DownloadDir)
}