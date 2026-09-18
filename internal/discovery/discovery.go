package discovery

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
	"github.com/God-Send/God-Send/internal/config"
	"github.com/God-Send/God-Send/internal/network"
	"github.com/God-Send/God-Send/internal/protocol"
)

// Discovery 设备发现服务
type Discovery struct {
	config    *config.Config
	deviceID  string
	service   *mdns.MDNSService
	server    *mdns.Server
	peers     map[string]*protocol.DeviceInfo
	peersMu   sync.RWMutex
	onPeerAdd func(*protocol.DeviceInfo)
	onPeerDel func(string)
	ctx       context.Context
	cancel    context.CancelFunc
}

// New 创建设备发现服务
func New(cfg *config.Config) *Discovery {
	deviceID := generateDeviceID()
	ctx, cancel := context.WithCancel(context.Background())

	return &Discovery{
		config:   cfg,
		deviceID: deviceID,
		peers:    make(map[string]*protocol.DeviceInfo),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// GetDeviceID 获取本设备ID
func (d *Discovery) GetDeviceID() string {
	return d.deviceID
}

// SetCallbacks 设置回调函数
func (d *Discovery) SetCallbacks(onPeerAdd func(*protocol.DeviceInfo), onPeerDel func(string)) {
	d.onPeerAdd = onPeerAdd
	d.onPeerDel = onPeerDel
}

// Start 启动设备发现服务
func (d *Discovery) Start() error {
	ip, err := network.GetPrimaryIP()
	if err != nil {
		return fmt.Errorf("获取本机IP失败: %w", err)
	}

	// 创建mDNS服务
	service, err := mdns.NewMDNSService(
		d.deviceID,                    // 实例名(使用设备ID)
		config.MDNSServiceName,        // 服务类型
		config.MDNSDomain,             // 域
		"",                            // 主机名(自动)
		d.config.Port,                 // 端口
		[]net.IP{net.ParseIP(ip)},     // IP地址
		[]string{                      // TXT记录
			"path=/",
			"device_name=" + d.config.DeviceName,
			"device_id=" + d.deviceID,
			"platform=" + getPlatform(),
			"color=" + d.config.AvatarColor,
		},
	)
	if err != nil {
		return fmt.Errorf("创建mDNS服务失败: %w", err)
	}

	d.service = service

	// 创建mDNS服务器
	server, err := mdns.NewServer(&mdns.Config{Zone: service, Iface: nil})
	if err != nil {
		return fmt.Errorf("启动mDNS服务器失败: %w", err)
	}
	d.server = server

	log.Printf("[发现] mDNS服务已启动，设备: %s, IP: %s, 端口: %d", d.config.DeviceName, ip, d.config.Port)

	// 启动设备扫描
	go d.scanLoop()

	return nil
}

// Stop 停止设备发现服务
func (d *Discovery) Stop() {
	d.cancel()
	if d.server != nil {
		d.server.Shutdown()
	}
	log.Println("[发现] mDNS服务已停止")
}

// GetPeers 获取已发现的设备列表
func (d *Discovery) GetPeers() []*protocol.DeviceInfo {
	d.peersMu.RLock()
	defer d.peersMu.RUnlock()

	peers := make([]*protocol.DeviceInfo, 0, len(d.peers))
	for _, peer := range d.peers {
		peers = append(peers, peer)
	}
	return peers
}

// scanLoop 扫描循环
func (d *Discovery) scanLoop() {
	// 初始扫描
	d.scan()

	// 定期扫描
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.scan()
			d.cleanupStalePeers()
		}
	}
}

// scan 执行一次设备扫描
func (d *Discovery) scan() {
	entriesCh := make(chan *mdns.ServiceEntry, 100)

	go func() {
		params := &mdns.QueryParam{
			Service:             config.MDNSServiceName,
			Domain:              config.MDNSDomain,
			Timeout:             3 * time.Second,
			Entries:             entriesCh,
			WantUnicastResponse: false,
		}
		mdns.Query(params)
		close(entriesCh)
	}()

	for entry := range entriesCh {
		if entry == nil {
			continue
		}

		// 跳过自身
		if entry.Name == d.deviceID {
			continue
		}

		// 解析设备信息
		peer := d.parseEntry(entry)
		if peer == nil {
			continue
		}

		d.addOrUpdatePeer(peer)
	}
}

// parseEntry 解析mDNS条目
func (d *Discovery) parseEntry(entry *mdns.ServiceEntry) *protocol.DeviceInfo {
	peer := &protocol.DeviceInfo{
		Port:     entry.Port,
		LastSeen: time.Now(),
	}

	// 获取IP地址
	if len(entry.AddrV4) > 0 {
		peer.IPAddress = entry.AddrV4.String()
	} else if len(entry.AddrV6) > 0 {
		peer.IPAddress = entry.AddrV6.String()
	}

	if peer.IPAddress == "" {
		return nil
	}

	// 解析TXT记录
	for _, txt := range entry.InfoFields {
		parts := strings.SplitN(txt, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := parts[0], parts[1]

		switch key {
		case "device_name":
			peer.Name = value
		case "device_id":
			peer.ID = value
		case "platform":
			peer.Platform = value
		case "color":
			peer.AvatarColor = value
		}
	}

	// 如果没有从TXT记录获取到ID，使用实例名
	if peer.ID == "" {
		peer.ID = entry.Name
	}

	// 如果没有名称，使用ID
	if peer.Name == "" {
		peer.Name = peer.ID
	}

	return peer
}

// addOrUpdatePeer 添加或更新设备
func (d *Discovery) addOrUpdatePeer(peer *protocol.DeviceInfo) {
	d.peersMu.Lock()
	defer d.peersMu.Unlock()

	existing, exists := d.peers[peer.ID]
	if exists {
		// 更新已有设备
		existing.IPAddress = peer.IPAddress
		existing.Port = peer.Port
		existing.LastSeen = peer.LastSeen
		existing.Name = peer.Name
		existing.Platform = peer.Platform
		existing.AvatarColor = peer.AvatarColor
	} else {
		// 新设备
		d.peers[peer.ID] = peer
		if d.onPeerAdd != nil {
			go d.onPeerAdd(peer)
		}
		log.Printf("[发现] 新设备上线: %s (%s:%d)", peer.Name, peer.IPAddress, peer.Port)
	}
}

// cleanupStalePeers 清理过期设备
func (d *Discovery) cleanupStalePeers() {
	d.peersMu.Lock()
	defer d.peersMu.Unlock()

	timeout := 30 * time.Second
	now := time.Now()

	for id, peer := range d.peers {
		if now.Sub(peer.LastSeen) > timeout {
			delete(d.peers, id)
			if d.onPeerDel != nil {
				go d.onPeerDel(id)
			}
			log.Printf("[发现] 设备离线: %s", peer.Name)
		}
	}
}

// generateDeviceID 生成设备ID
func generateDeviceID() string {
	mac := network.GetMACAddress()
	hash := sha256.Sum256([]byte(mac))
	return fmt.Sprintf("%x", hash[:8])
}

// getPlatform 获取平台信息
func getPlatform() string {
	return "unknown"
}