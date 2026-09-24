package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
)

// Device 发现的设备信息
type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	IP        string    `json:"ip"`
	Port      int       `json:"port"`
	Online    bool      `json:"online"`
	LastSeen  time.Time `json:"lastSeen"`
	OS        string    `json:"os"`
	Unread    int       `json:"unread"`
	LastMsg   string    `json:"lastMsg"`
	LastTime  string    `json:"lastTime"`
	RelayIP   string    `json:"relayIp,omitempty"` // 中继桌面IP（跨桌面web设备）
}

// Discovery 设备发现服务
type Discovery struct {
	mu             sync.RWMutex
	devices        map[string]*Device // UDP发现的设备
	webDevices     map[string]*Device // 浏览器WS连接的设备
	config         *Config
	localIPs       []string
	udpConn        *net.UDPConn
	broadcastPort  int
	onDeviceChange func()
	stopCh         chan struct{}
	mdnsService    *mdns.MDNSService       // mDNS服务
	mdnsServer     *mdns.Server            // mDNS服务器
	mdnsCh         chan *mdns.ServiceEntry // mDNS发现通道
}

// NewDiscovery 创建设备发现服务
func NewDiscovery(cfg *Config) *Discovery {
	return &Discovery{
		devices:       make(map[string]*Device),
		webDevices:    make(map[string]*Device),
		config:        cfg,
		broadcastPort: cfg.GetPort() + 1, // 广播端口 = QUIC端口 + 1
		stopCh:        make(chan struct{}),
	}
}

// SetOnDeviceChange 设置设备变化回调
func (d *Discovery) SetOnDeviceChange(fn func()) {
	d.onDeviceChange = fn
}

// Start 启动设备发现
func (d *Discovery) Start() error {
	d.localIPs = d.getLocalIPs()

	// 启动UDP监听
	addr := &net.UDPAddr{Port: d.broadcastPort}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return fmt.Errorf("监听UDP失败: %v", err)
	}
	d.udpConn = conn

	// 启动mDNS服务
	d.startMDNS()

	// 启动广播协程
	go d.broadcastLoop()
	// 启动监听协程
	go d.listenLoop()
	// 启动离线检测协程
	go d.offlineCheckLoop()
	// 启动mDNS浏览协程
	go d.mdnsBrowseLoop()

	log.Printf("[发现] 设备发现服务已启动, 广播端口: %d", d.broadcastPort)
	return nil
}

// Stop 停止设备发现
func (d *Discovery) Stop() {
	close(d.stopCh)
	if d.udpConn != nil {
		d.udpConn.Close()
	}
	// 停止mDNS
	if d.mdnsServer != nil {
		d.mdnsServer.Shutdown()
	}
	// 发送离线通知
	d.sendOfflinePacket()
}

// broadcastLoop 定期广播心跳
func (d *Discovery) broadcastLoop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// 立即发送一次
	d.sendHeartbeat()

	for {
		select {
		case <-ticker.C:
			d.sendHeartbeat()
		case <-d.stopCh:
			return
		}
	}
}

// listenLoop 监听设备广播
func (d *Discovery) listenLoop() {
	buf := make([]byte, 1024)
	for {
		select {
		case <-d.stopCh:
			return
		default:
		}
		d.udpConn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, _, err := d.udpConn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		d.handlePacket(buf[:n])
	}
}

// offlineCheckLoop 检测离线设备
func (d *Discovery) offlineCheckLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.checkOffline()
		case <-d.stopCh:
			return
		}
	}
}

// sendHeartbeat 发送心跳包
func (d *Discovery) sendHeartbeat() {
	pkt := DiscoveryPacket{
		DeviceID:   d.config.GetDeviceID(),
		DeviceName: d.config.GetDeviceName(),
		Port:       d.config.GetPort(),
		Action:     "heartbeat",
	}
	// 获取本机IP
	if len(d.localIPs) > 0 {
		pkt.IP = d.localIPs[0]
	}
	d.sendBroadcast(&pkt)
}

// sendOfflinePacket 发送离线通知
func (d *Discovery) sendOfflinePacket() {
	pkt := DiscoveryPacket{
		DeviceID:   d.config.GetDeviceID(),
		DeviceName: d.config.GetDeviceName(),
		Port:       d.config.GetPort(),
		Action:     "offline",
	}
	if len(d.localIPs) > 0 {
		pkt.IP = d.localIPs[0]
	}
	d.sendBroadcast(&pkt)
}

// sendBroadcast 发送广播包
func (d *Discovery) sendBroadcast(pkt *DiscoveryPacket) {
	data, err := json.Marshal(pkt)
	if err != nil {
		return
	}
	broadcastAddr := &net.UDPAddr{
		IP:   net.IPv4(255, 255, 255, 255),
		Port: d.broadcastPort,
	}
	d.udpConn.WriteToUDP(data, broadcastAddr)
}

// handlePacket 处理接收到的广播包
func (d *Discovery) handlePacket(data []byte) {
	var pkt DiscoveryPacket
	if err := json.Unmarshal(data, &pkt); err != nil {
		return
	}
	// 忽略自己的包
	if pkt.DeviceID == d.config.GetDeviceID() {
		return
	}

	var changed bool
	d.mu.Lock()
	switch pkt.Action {
	case "heartbeat", "online":
		dev, exists := d.devices[pkt.DeviceID]
		if exists {
			dev.Name = pkt.DeviceName
			dev.IP = pkt.IP
			dev.Port = pkt.Port
			dev.Online = true
			dev.LastSeen = time.Now()
		} else {
			d.devices[pkt.DeviceID] = &Device{
				ID:       pkt.DeviceID,
				Name:     pkt.DeviceName,
				IP:       pkt.IP,
				Port:     pkt.Port,
				Online:   true,
				LastSeen: time.Now(),
				OS:       "Desktop",
			}
			changed = true
		}
	case "offline":
		if dev, exists := d.devices[pkt.DeviceID]; exists {
			dev.Online = false
			changed = true
		}
	case "webJoin":
		// 其他桌面广播的web设备上线
		if pkt.WebDeviceID != "" {
			d.webDevices[pkt.WebDeviceID] = &Device{
				ID:       pkt.WebDeviceID,
				Name:     pkt.WebDeviceName,
				IP:       pkt.WebDeviceIP,
				Port:     0,
				Online:   true,
				LastSeen: time.Now(),
				OS:       "Web",
				RelayIP:  pkt.IP, // 中继桌面IP
			}
			changed = true
		}
	case "webLeave":
		if pkt.WebDeviceID != "" {
			if _, exists := d.webDevices[pkt.WebDeviceID]; exists {
				delete(d.webDevices, pkt.WebDeviceID)
				changed = true
			}
		}
	}
	d.mu.Unlock()

	// 在锁外调用回调，避免死锁
	if changed && d.onDeviceChange != nil {
		d.onDeviceChange()
	}
}

// checkOffline 检查离线设备
func (d *Discovery) checkOffline() {
	var changed bool
	d.mu.Lock()
	now := time.Now()
	for _, dev := range d.devices {
		if dev.Online && now.Sub(dev.LastSeen) > 10*time.Second {
			dev.Online = false
			changed = true
		}
	}
	d.mu.Unlock()
	// 在锁外调用回调，避免死锁
	if changed && d.onDeviceChange != nil {
		d.onDeviceChange()
	}
}

// GetDevices 获取设备列表（包含自身 + UDP发现设备 + Web客户端）
func (d *Discovery) GetDevices() []Device {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]Device, 0, len(d.devices)+len(d.webDevices)+1)

	// 内联自身设备（避免调用GetSelfDevice导致RLock重入死锁）
	if len(d.localIPs) > 0 {
		result = append(result, Device{
			ID:       d.config.GetDeviceID(),
			Name:     d.config.GetDeviceName(),
			IP:       d.localIPs[0],
			Port:     d.config.GetPort(),
			Online:   true,
			LastSeen: time.Now(),
			OS:       "Desktop",
		})
	}

	// 添加UDP发现的设备
	for _, dev := range d.devices {
		result = append(result, *dev)
	}

	// 添加Web客户端设备
	for _, dev := range d.webDevices {
		result = append(result, *dev)
	}

	return result
}

// GetSelfDevice 获取自身设备信息
func (d *Discovery) GetSelfDevice() *Device {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.localIPs) == 0 {
		return nil
	}
	return &Device{
		ID:       d.config.GetDeviceID(),
		Name:     d.config.GetDeviceName(),
		IP:       d.localIPs[0],
		Port:     d.config.GetPort(),
		Online:   true,
		LastSeen: time.Now(),
		OS:       "Desktop",
	}
}

// AddWebDevice 添加Web客户端设备
func (d *Discovery) AddWebDevice(id, name, ip string) {
	d.mu.Lock()
	d.webDevices[id] = &Device{
		ID:       id,
		Name:     name,
		IP:       ip,
		Port:     0,
		Online:   true,
		LastSeen: time.Now(),
		OS:       "Web",
	}
	d.mu.Unlock()

	// 广播web设备上线给其他桌面
	d.sendWebDeviceBroadcast("webJoin", id, name, ip)

	// 在锁外调用回调，避免死锁
	if d.onDeviceChange != nil {
		d.onDeviceChange()
	}
}

// RemoveWebDevice 移除Web客户端设备
func (d *Discovery) RemoveWebDevice(id string) {
	d.mu.Lock()
	dev, exists := d.webDevices[id]
	if exists {
		delete(d.webDevices, id)
	}
	d.mu.Unlock()

	if exists {
		// 广播web设备离线给其他桌面
		ip := ""
		if dev != nil {
			ip = dev.IP
		}
		d.sendWebDeviceBroadcast("webLeave", id, "", ip)

		// 在锁外调用回调，避免死锁
		if d.onDeviceChange != nil {
			d.onDeviceChange()
		}
	}
}

// sendWebDeviceBroadcast 广播web设备上/下线给其他桌面
func (d *Discovery) sendWebDeviceBroadcast(action, webID, webName, webIP string) {
	pkt := DiscoveryPacket{
		DeviceID:      d.config.GetDeviceID(),
		DeviceName:    d.config.GetDeviceName(),
		IP:            "",
		Port:          d.config.GetPort(),
		Action:        action,
		WebDeviceID:   webID,
		WebDeviceName: webName,
		WebDeviceIP:   webIP,
	}
	if len(d.localIPs) > 0 {
		pkt.IP = d.localIPs[0]
	}
	d.sendBroadcast(&pkt)
}

// IsWebDevice 检查是否是Web客户端设备
func (d *Discovery) IsWebDevice(id string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.webDevices[id]
	return ok
}

// IsLocalWebDevice 检查是否是本机连接的Web客户端（非跨桌面relay）
func (d *Discovery) IsLocalWebDevice(id string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	dev, ok := d.webDevices[id]
	if !ok {
		return false
	}
	return dev.RelayIP == ""
}

// GetDeviceRelay 获取web设备的中继信息（relayIP, relayPort, isRelay）
func (d *Discovery) GetDeviceRelay(deviceID string) (string, int, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	dev, ok := d.webDevices[deviceID]
	if !ok || dev.RelayIP == "" {
		return "", 0, false
	}
	// 查找中继桌面的端口
	for _, d2 := range d.devices {
		if d2.IP == dev.RelayIP {
			return dev.RelayIP, d2.Port, true
		}
	}
	// 默认使用配置的端口
	return dev.RelayIP, d.config.GetPort(), true
}

// UpdateWebDeviceName 更新Web客户端设备名
func (d *Discovery) UpdateWebDeviceName(id, name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if dev, ok := d.webDevices[id]; ok {
		dev.Name = name
	}
}

// GetDevice 获取指定设备
func (d *Discovery) GetDevice(id string) *Device {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if dev, ok := d.devices[id]; ok {
		cp := *dev
		return &cp
	}
	return nil
}

// getLocalIPs 获取本机IP
func (d *Discovery) getLocalIPs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	return ips
}

// UpdateUnread 更新未读数
func (d *Discovery) UpdateUnread(deviceID string, count int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if dev, ok := d.devices[deviceID]; ok {
		dev.Unread = count
	}
}

// IncUnread 增加未读数
func (d *Discovery) IncUnread(deviceID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if dev, ok := d.devices[deviceID]; ok {
		dev.Unread++
	}
}

// ClearUnread 清除未读
func (d *Discovery) ClearUnread(deviceID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if dev, ok := d.devices[deviceID]; ok {
		dev.Unread = 0
	}
}

// startMDNS 启动mDNS服务注册
func (d *Discovery) startMDNS() {
	// 注册mDNS服务：_godsend._tcp
	service, err := mdns.NewMDNSService(
		d.config.GetDeviceID(), // 实例名（使用设备ID保证唯一）
		"_godsend._tcp",        // 服务类型
		"",                     // 域
		"",                     // 主机名
		d.config.HTTPPort,      // 端口
		[]net.IP{},             // IP（空=自动检测）
		[]string{               // TXT记录
			"deviceName=" + d.config.GetDeviceName(),
			"quicPort=" + strconv.Itoa(d.config.GetPort()),
			"deviceId=" + d.config.GetDeviceID(),
		},
	)
	if err != nil {
		log.Printf("[发现] mDNS服务创建失败: %v", err)
		return
	}

	d.mdnsService = service

	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		log.Printf("[发现] mDNS服务器启动失败: %v", err)
		return
	}
	d.mdnsServer = server

	// 创建发现通道
	d.mdnsCh = make(chan *mdns.ServiceEntry, 32)

	log.Printf("[发现] mDNS服务已注册: _godsend._tcp 端口 %d", d.config.HTTPPort)
}

// mdnsBrowseLoop mDNS浏览循环
func (d *Discovery) mdnsBrowseLoop() {
	// 定期发起mDNS查询
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// 立即查询一次
	d.mdnsQuery()

	for {
		select {
		case <-ticker.C:
			d.mdnsQuery()
		case entry := <-d.mdnsCh:
			d.handleMDNSEntry(entry)
		case <-d.stopCh:
			return
		}
	}
}

// mdnsQuery 发起mDNS查询
func (d *Discovery) mdnsQuery() {
	params := &mdns.QueryParam{
		Service:             "_godsend._tcp",
		Domain:              "local",
		Timeout:             3 * time.Second,
		Entries:             d.mdnsCh,
		WantUnicastResponse: false,
	}
	go func() {
		if err := mdns.Query(params); err != nil {
			log.Printf("[发现] mDNS查询失败: %v", err)
		}
	}()
}

// handleMDNSEntry 处理mDNS发现的服务
func (d *Discovery) handleMDNSEntry(entry *mdns.ServiceEntry) {
	// 从TXT记录中提取设备信息
	var deviceName, deviceID string
	var quicPort int
	for _, txt := range entry.InfoFields {
		parts := strings.SplitN(txt, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "deviceName":
			deviceName = parts[1]
		case "deviceId":
			deviceID = parts[1]
		case "quicPort":
			quicPort, _ = strconv.Atoi(parts[1])
		}
	}

	// 忽略自己的服务
	if deviceID == d.config.GetDeviceID() {
		return
	}
	if deviceID == "" {
		deviceID = entry.Name
	}
	if deviceName == "" {
		deviceName = entry.Name
	}
	if quicPort == 0 {
		quicPort = 53333 // 默认QUIC端口
	}

	// 获取IP地址
	var ip string
	if len(entry.AddrV4) > 0 {
		ip = entry.AddrV4.String()
	} else if len(entry.AddrV6) > 0 {
		ip = entry.AddrV6.String()
	}
	if ip == "" {
		return
	}

	// 更新设备列表
	d.mu.Lock()
	dev, exists := d.devices[deviceID]
	if exists {
		dev.Name = deviceName
		dev.IP = ip
		dev.Port = quicPort
		dev.Online = true
		dev.LastSeen = time.Now()
	} else {
		d.devices[deviceID] = &Device{
			ID:       deviceID,
			Name:     deviceName,
			IP:       ip,
			Port:     quicPort,
			Online:   true,
			LastSeen: time.Now(),
			OS:       "Desktop",
		}
	}
	d.mu.Unlock()

	// 在锁外调用回调，避免死锁
	if d.onDeviceChange != nil {
		d.onDeviceChange()
	}

	log.Printf("[发现] mDNS发现设备: %s (%s) IP:%s", deviceName, deviceID, ip)
}