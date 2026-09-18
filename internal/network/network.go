package network

import (
	"fmt"
	"net"
	"strings"
)

// GetLocalIPs 获取本机所有局域网IP地址
func GetLocalIPs() ([]string, error) {
	var ips []string

	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("获取网络接口失败: %w", err)
	}

	for _, iface := range interfaces {
		// 跳过回环接口和未启用的接口
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// 只保留IPv4地址
			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				ips = append(ips, ip.String())
			}
		}
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("未找到可用的局域网IP地址")
	}

	return ips, nil
}

// GetPrimaryIP 获取首选IP地址
func GetPrimaryIP() (string, error) {
	ips, err := GetLocalIPs()
	if err != nil {
		return "", err
	}
	return ips[0], nil
}

// IsPrivateIP 检查是否为私有IP地址
func IsPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	privateRanges := []struct {
		network *net.IPNet
	}{
		{parseCIDR("10.0.0.0/8")},
		{parseCIDR("172.16.0.0/12")},
		{parseCIDR("192.168.0.0/16")},
		{parseCIDR("169.254.0.0/16")},
	}

	for _, r := range privateRanges {
		if r.network != nil && r.network.Contains(ip) {
			return true
		}
	}
	return false
}

// parseCIDR 解析CIDR
func parseCIDR(s string) *net.IPNet {
	_, network, err := net.ParseCIDR(s)
	if err != nil {
		return nil
	}
	return network
}

// GetAvailablePort 获取可用端口
func GetAvailablePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port, nil
}

// IsPortAvailable 检查端口是否可用
func IsPortAvailable(port int) bool {
	addr := fmt.Sprintf("localhost:%d", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// GetMACAddress 获取MAC地址(用于生成设备ID)
func GetMACAddress() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "unknown"
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		if len(iface.HardwareAddr) > 0 {
			return strings.ToUpper(iface.HardwareAddr.String())
		}
	}

	return "unknown"
}

// FormatSpeed 格式化传输速度
func FormatSpeed(bytesPerSecond float64) string {
	switch {
	case bytesPerSecond >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB/s", bytesPerSecond/(1024*1024*1024))
	case bytesPerSecond >= 1024*1024:
		return fmt.Sprintf("%.1f MB/s", bytesPerSecond/(1024*1024))
	case bytesPerSecond >= 1024:
		return fmt.Sprintf("%.1f KB/s", bytesPerSecond/1024)
	default:
		return fmt.Sprintf("%.0f B/s", bytesPerSecond)
	}
}

// FormatSize 格式化文件大小
func FormatSize(bytes int64) string {
	switch {
	case bytes >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB", float64(bytes)/(1024*1024*1024))
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}