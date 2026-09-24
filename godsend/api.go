package godsend

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsClientInfo WebSocket客户端信息
type wsClientInfo struct {
	deviceID   string
	deviceName string
	isDesktop  bool // true=桌面WebView2, false=浏览器客户端
	remoteIP   string
}

// API HTTP API和WebSocket服务
type API struct {
	mu          sync.RWMutex
	config      *Config
	discovery   *Discovery
	network     *Network
	chatHistory map[string][]*ChatRecord      // deviceID -> messages
	wsClients   map[*websocket.Conn]*wsClientInfo // WS连接 -> 客户端信息
	upgrader    websocket.Upgrader
	httpServer  *http.Server
	onSendMessage func(msg *Message)
}

// NewAPI 创建API服务
func NewAPI(cfg *Config, disc *Discovery, net *Network) *API {
	return &API{
		config:      cfg,
		discovery:   disc,
		network:     net,
		chatHistory: make(map[string][]*ChatRecord),
		wsClients:   make(map[*websocket.Conn]*wsClientInfo),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// SetOnSendMessage 设置发送消息回调
func (a *API) SetOnSendMessage(fn func(*Message)) {
	a.onSendMessage = fn
}

// GetHandler 获取HTTP Handler（供Wails AssetServer使用）
func (a *API) GetHandler() http.Handler {
	if a.httpServer != nil {
		return a.httpServer.Handler
	}
	// 如果HTTP服务尚未启动，创建mux并返回
	mux := http.NewServeMux()
	a.registerRoutes(mux)
	return mux
}

// registerRoutes 注册HTTP路由
func (a *API) registerRoutes(mux *http.ServeMux) {
	// 静态文件
	mux.HandleFunc("/", a.handleIndex)

	// API路由
	mux.HandleFunc("/api/devices", a.handleDevices)
	mux.HandleFunc("/api/config", a.handleConfig)
	mux.HandleFunc("/api/config/update", a.handleConfigUpdate)
	mux.HandleFunc("/api/send", a.handleSend)
	mux.HandleFunc("/api/upload", a.handleUpload)
	mux.HandleFunc("/api/history/", a.handleHistory)
	mux.HandleFunc("/api/files/", a.handleFiles)
	mux.HandleFunc("/ws", a.handleWebSocket)
}

// Start 启动HTTP服务
func (a *API) Start() error {
	mux := http.NewServeMux()
	a.registerRoutes(mux)

	port := a.config.HTTPPort
	// 始终监听0.0.0.0，允许局域网浏览器访问
	listenAddr := fmt.Sprintf("0.0.0.0:%d", port)
	a.httpServer = &http.Server{
		Addr:    listenAddr,
		Handler: mux,
	}

	go func() {
		log.Printf("[API] HTTP服务已启动, 地址: %s", listenAddr)
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[API] HTTP服务错误: %v", err)
		}
	}()

	return nil
}

// Stop 停止API服务
func (a *API) Stop() {
	if a.httpServer != nil {
		a.httpServer.Close()
	}
	a.mu.Lock()
	for conn := range a.wsClients {
		conn.Close()
	}
	a.wsClients = make(map[*websocket.Conn]*wsClientInfo)
	a.mu.Unlock()
}

// handleIndex 处理首页
func (a *API) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}

// handleDevices 获取设备列表
func (a *API) handleDevices(w http.ResponseWriter, r *http.Request) {
	devices := a.discovery.GetDevices()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(devices)
}

// handleConfig 获取配置
func (a *API) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"deviceId":   a.config.GetDeviceID(),
		"deviceName": a.config.GetDeviceName(),
		"port":       a.config.GetPort(),
		"httpPort":   a.config.GetHTTPPort(),
		"savePath":   a.config.GetSavePath(),
		"webMode":    a.config.GetWebMode(),
	})
}

// handleConfigUpdate 更新配置
func (a *API) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	var req struct {
		DeviceName string `json:"deviceName"`
		Port       int    `json:"port"`
		HTTPPort   int    `json:"httpPort"`
		SavePath   string `json:"savePath"`
		WebMode    bool   `json:"webMode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// 记录webMode变化
	oldWebMode := a.config.GetWebMode()

	if err := a.config.Update(req.DeviceName, req.Port, req.HTTPPort, req.SavePath, req.WebMode); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// webMode从开启变为关闭时，断开所有浏览器WS连接
	if oldWebMode && !req.WebMode {
		a.disconnectBrowserClients()
		log.Println("[API] 网页版模式已关闭，已断开所有浏览器连接")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleSend 发送消息
func (a *API) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	var req struct {
		To       string `json:"to"`
		Content  string `json:"content"`
		Type     string `json:"type"`
		FromID   string `json:"fromId"`
		FromName string `json:"fromName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	fromID := a.config.GetDeviceID()
	fromName := a.config.GetDeviceName()
	if req.FromID != "" {
		fromID = req.FromID
		fromName = req.FromName
	}

	msg := &Message{
		ID:      fmt.Sprintf("%d", time.Now().UnixNano()),
		Type:    MessageType(req.Type),
		From:    fromID,
		FromName: fromName,
		To:      req.To,
		Content: req.Content,
		Time:    time.Now().UnixMilli(),
	}

	if msg.Type == "" {
		msg.Type = MsgText
	}

	// 保存到历史
	record := &ChatRecord{
		ID:       msg.ID,
		From:     fromID,
		FromName: fromName,
		To:       req.To,
		Type:     string(msg.Type),
		Content:  req.Content,
		Time:     msg.Time,
		Status:   "sending",
	}
	a.addHistory(req.To, record)

	// 消息路由
	if req.To == a.config.GetDeviceID() {
		record.Status = "delivered"
		a.broadcastWS(WSMessage{Type: "messageSent", Data: record})
		incomingRecord := &ChatRecord{
			ID:       msg.ID,
			From:     fromID,
			FromName: fromName,
			To:       req.To,
			Type:     string(msg.Type),
			Content:  req.Content,
			Time:     msg.Time,
			Status:   "delivered",
		}
		a.addHistory(fromID, incomingRecord)
		a.discovery.IncUnread(fromID)
		a.broadcastWS(WSMessage{Type: "newMessage", Data: incomingRecord})
	} else if a.discovery.IsWebDevice(req.To) {
		record.Status = "sent"
		a.sendToWebDevice(req.To, WSMessage{Type: "newMessage", Data: record})
		a.broadcastWS(WSMessage{Type: "messageSent", Data: record})
	} else {
		err := a.network.SendMessage(msg)
		if err != nil {
			record.Status = "failed"
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
			return
		}
		record.Status = "sent"
		a.broadcastWS(WSMessage{Type: "messageSent", Data: record})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "id": msg.ID})
}

// handleUpload 处理文件上传
func (a *API) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	toDevice := r.FormValue("to")
	if toDevice == "" {
		http.Error(w, "missing 'to' parameter", 400)
		return
	}

	// 支持浏览器客户端指定发送者ID
	fromID := r.FormValue("fromId")
	fromName := r.FormValue("fromName")
	if fromID == "" {
		fromID = a.config.GetDeviceID()
		fromName = a.config.GetDeviceName()
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer file.Close()

	// 保存到临时目录
	tmpDir := filepath.Join(os.TempDir(), "god-send-upload")
	os.MkdirAll(tmpDir, 0755)
	tmpPath := filepath.Join(tmpDir, header.Filename)
	out, err := os.Create(tmpPath)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer out.Close()

	written, err := io.Copy(out, file)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	fileID := fmt.Sprintf("%d", time.Now().UnixNano())

	// 判断目标设备类型，路由文件传输
	if toDevice == a.config.GetDeviceID() {
		// 发给本机 — 保存到下载目录
		savePath := a.config.GetSavePath()
		os.MkdirAll(savePath, 0755)
		finalPath := filepath.Join(savePath, header.Filename)
		// 复制临时文件到最终位置
		src, _ := os.Open(tmpPath)
		if src != nil {
			dst, err := os.Create(finalPath)
			if err == nil {
				io.Copy(dst, src)
				dst.Close()
			}
			src.Close()
		}
		os.Remove(tmpPath)

		record := &ChatRecord{
			ID:       fileID,
			From:     fromID,
			FromName: fromName,
			To:       toDevice,
			Type:     "file",
			Content:  header.Filename,
			Time:     time.Now().UnixMilli(),
			FileID:   fileID,
			FileName: header.Filename,
			FileSize: written,
			FileUrl:  "/api/files/" + header.Filename,
			Status:   "delivered",
		}
		a.addHistory(fromID, record)
		a.broadcastWS(WSMessage{Type: "newMessage", Data: record})
	} else if a.discovery.IsWebDevice(toDevice) {
		// 发给Web客户端 — 保存文件到下载目录，通过WS通知可下载
		savePath := a.config.GetSavePath()
		os.MkdirAll(savePath, 0755)
		finalPath := filepath.Join(savePath, header.Filename)
		// 复制临时文件到下载目录
		src, _ := os.Open(tmpPath)
		if src != nil {
			dst, err := os.Create(finalPath)
			if err == nil {
				io.Copy(dst, src)
				dst.Close()
			}
			src.Close()
		}
		os.Remove(tmpPath)

		record := &ChatRecord{
			ID:       fileID,
			From:     fromID,
			FromName: fromName,
			To:       toDevice,
			Type:     "file",
			Content:  header.Filename,
			Time:     time.Now().UnixMilli(),
			FileID:   fileID,
			FileName: header.Filename,
			FileSize: written,
			FileUrl:  "/api/files/" + header.Filename,
			Status:   "sent",
		}
		a.addHistory(toDevice, record)
		a.sendToWebDevice(toDevice, WSMessage{Type: "newMessage", Data: record})
		a.broadcastWS(WSMessage{Type: "messageSent", Data: record})
	} else {
		// 发给QUIC设备 — 通过QUIC转发
		go func() {
			err := a.network.SendFile(toDevice, tmpPath, fileID)
			os.Remove(tmpPath)
			if err != nil {
				log.Printf("[API] 文件发送失败: %v", err)
			}
		}()
		record := &ChatRecord{
			ID:       fileID,
			From:     fromID,
			FromName: fromName,
			To:       toDevice,
			Type:     "file",
			Content:  header.Filename,
			Time:     time.Now().UnixMilli(),
			FileID:   fileID,
			FileName: header.Filename,
			FileSize: written,
			Status:   "sending",
		}
		a.addHistory(toDevice, record)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "fileId": fileID})
}

// handleHistory 获取聊天记录
func (a *API) handleHistory(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimPrefix(r.URL.Path, "/api/history/")
	a.mu.RLock()
	history := a.chatHistory[deviceID]
	a.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

// handleFiles 提供文件下载
func (a *API) handleFiles(w http.ResponseWriter, r *http.Request) {
	fileName := strings.TrimPrefix(r.URL.Path, "/api/files/")
	savePath := a.config.GetSavePath()
	filePath := filepath.Join(savePath, fileName)
	// 设置Content-Disposition让浏览器下载而非打开
	w.Header().Set("Content-Disposition", "attachment; filename=\""+fileName+"\"")
	http.ServeFile(w, r, filePath)
}

// handleWebSocket WebSocket处理
func (a *API) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 判断是桌面客户端还是浏览器客户端
	isDesktop := isLocalRequest(r)

	// 非桌面客户端且网页版模式关闭时，拒绝连接
	if !isDesktop && !a.config.GetWebMode() {
		http.Error(w, "Web mode is disabled", http.StatusForbidden)
		return
	}

	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] 升级失败: %v", err)
		return
	}

	remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)

	var client *wsClientInfo
	if isDesktop {
		// 桌面WebView2客户端
		client = &wsClientInfo{
			deviceID:   a.config.GetDeviceID(),
			deviceName: a.config.GetDeviceName(),
			isDesktop:  true,
			remoteIP:   remoteIP,
		}
	} else {
		// 浏览器客户端 - 注册为Web设备
		deviceID := fmt.Sprintf("web-%s", generateShortID())
		deviceName := parseBrowserName(r.UserAgent())
		client = &wsClientInfo{
			deviceID:   deviceID,
			deviceName: deviceName,
			isDesktop:  false,
			remoteIP:   remoteIP,
		}
		a.discovery.AddWebDevice(deviceID, deviceName, remoteIP)
		log.Printf("[WS] 浏览器客户端已连接: %s (%s) from %s", deviceName, deviceID, remoteIP)
	}

	a.mu.Lock()
	a.wsClients[conn] = client
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		info, exists := a.wsClients[conn]
	 delete(a.wsClients, conn)
		a.mu.Unlock()
		// 移除Web设备
		if exists && !info.isDesktop {
			a.discovery.RemoveWebDevice(info.deviceID)
			log.Printf("[WS] 浏览器客户端已断开: %s (%s)", info.deviceName, info.deviceID)
		}
		conn.Close()
	}()

	// 发送初始状态
	a.sendInitialState(conn, client)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}
		a.handleWSMessage(conn, message)
	}
}

// isLocalRequest 判断是否是本地请求（桌面WebView2）
func isLocalRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// parseBrowserName 从User-Agent解析浏览器名称
func parseBrowserName(ua string) string {
	if strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad") {
	return "iPhone浏览器"
	}
	if strings.Contains(ua, "Android") {
	return "Android浏览器"
	}
	if strings.Contains(ua, "Edg/") {
	return "Edge浏览器"
	}
	if strings.Contains(ua, "Chrome") {
	return "Chrome浏览器"
	}
	if strings.Contains(ua, "Firefox") {
	return "Firefox浏览器"
	}
	if strings.Contains(ua, "Safari") {
	return "Safari浏览器"
	}
	return "网页客户端"
}

// generateShortID 生成短ID
func generateShortID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%x%x", b[:2], b[2:])
}

// sendInitialState 发送初始状态
func (a *API) sendInitialState(conn *websocket.Conn, client *wsClientInfo) {
	// 根据客户端类型发送不同的selfInfo
	if client.isDesktop {
		// 桌面客户端 - 发送服务器自身信息
		a.sendWS(conn, WSMessage{
			Type: "selfInfo",
			Data: map[string]interface{}{
				"deviceId":   a.config.GetDeviceID(),
				"deviceName": a.config.GetDeviceName(),
				"port":       a.config.GetPort(),
				"httpPort":   a.config.GetHTTPPort(),
				"savePath":   a.config.GetSavePath(),
				"webMode":    a.config.GetWebMode(),
			},
		})
	} else {
		// 浏览器客户端 - 发送其被分配的设备信息
		a.sendWS(conn, WSMessage{
			Type: "selfInfo",
			Data: map[string]interface{}{
				"deviceId":   client.deviceID,
				"deviceName": client.deviceName,
				"port":       a.config.GetPort(),
				"httpPort":   a.config.GetHTTPPort(),
				"savePath":   a.config.GetSavePath(),
				"webMode":    a.config.GetWebMode(),
			},
		})
	}

	// 发送设备列表
	a.sendWS(conn, WSMessage{
		Type: "devices",
		Data: a.discovery.GetDevices(),
	})
}

// handleWSMessage 处理WebSocket消息
func (a *API) handleWSMessage(conn *websocket.Conn, data []byte) {
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	switch msg.Type {
	case "sendMessage":
		a.handleWSSendMessage(conn, msg.Data)
	case "sendFile":
		a.handleWSSendFile(conn, msg.Data)
	case "clearUnread":
		a.handleWSClearUnread(msg.Data)
	case "join":
		a.handleWSJoin(conn, msg.Data)
	}
}

// handleWSJoin 处理浏览器客户端加入
func (a *API) handleWSJoin(conn *websocket.Conn, data interface{}) {
	a.mu.RLock()
	client, exists := a.wsClients[conn]
	a.mu.RUnlock()

	if !exists || client.isDesktop {
		return
	}

	jsonData, _ := json.Marshal(data)
	var req struct {
		DeviceName string `json:"deviceName"`
	}
	if err := json.Unmarshal(jsonData, &req); err != nil {
		return
	}

	if req.DeviceName != "" {
		client.deviceName = req.DeviceName
		a.discovery.UpdateWebDeviceName(client.deviceID, req.DeviceName)
		log.Printf("[WS] 浏览器客户端更新名称: %s -> %s", client.deviceID, req.DeviceName)
	}
}

// handleWSSendMessage 处理WebSocket发送消息
func (a *API) handleWSSendMessage(conn *websocket.Conn, data interface{}) {
	jsonData, _ := json.Marshal(data)
	var req struct {
		To      string `json:"to"`
		Content string `json:"content"`
		Type    string `json:"type"`
	}
	if err := json.Unmarshal(jsonData, &req); err != nil {
		return
	}

	// 获取发送者信息
	a.mu.RLock()
	client, clientExists := a.wsClients[conn]
	a.mu.RUnlock()

	fromID := a.config.GetDeviceID()
	fromName := a.config.GetDeviceName()
	if clientExists && !client.isDesktop {
		fromID = client.deviceID
		fromName = client.deviceName
	}

	msg := &Message{
		ID:      fmt.Sprintf("%d", time.Now().UnixNano()),
		Type:    MessageType(req.Type),
		From:    fromID,
		FromName: fromName,
		To:      req.To,
		Content: req.Content,
		Time:    time.Now().UnixMilli(),
	}
	if msg.Type == "" {
		msg.Type = MsgText
	}

	// 保存到历史
	record := &ChatRecord{
		ID:       msg.ID,
		From:     fromID,
		FromName: fromName,
		To:       req.To,
		Type:     string(msg.Type),
		Content:  req.Content,
		Time:     msg.Time,
		Status:   "sending",
	}
	a.addHistory(req.To, record)

	// 消息路由
	if req.To == a.config.GetDeviceID() {
		// 发给本机（桌面自身）— 本地处理
		record.Status = "delivered"
		a.broadcastWS(WSMessage{Type: "messageSent", Data: record})
		// 通知桌面客户端收到消息
		incomingRecord := &ChatRecord{
			ID:       msg.ID,
			From:     fromID,
			FromName: fromName,
			To:       req.To,
			Type:     string(msg.Type),
			Content:  req.Content,
			Time:     msg.Time,
			Status:   "delivered",
		}
		a.addHistory(fromID, incomingRecord)
		a.discovery.IncUnread(fromID)
		a.broadcastWS(WSMessage{Type: "newMessage", Data: incomingRecord})
	} else if a.discovery.IsWebDevice(req.To) {
		// 发给Web客户端 — 通过WS路由
		record.Status = "sent"
		a.sendToWebDevice(req.To, WSMessage{Type: "newMessage", Data: record})
		a.broadcastWS(WSMessage{Type: "messageSent", Data: record})
	} else {
		// 发给QUIC设备 — 通过QUIC转发
		err := a.network.SendMessage(msg)
		if err != nil {
			record.Status = "failed"
		} else {
			record.Status = "sent"
		}
		a.broadcastWS(WSMessage{Type: "messageSent", Data: record})
	}
}

// sendToWebDevice 发送消息给指定的Web客户端
func (a *API) sendToWebDevice(deviceID string, msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for conn, client := range a.wsClients {
		if client.deviceID == deviceID {
			conn.WriteMessage(websocket.TextMessage, data)
			return
		}
	}
}

// handleWSSendFile 处理WebSocket发送文件
func (a *API) handleWSSendFile(conn *websocket.Conn, data interface{}) {
	jsonData, _ := json.Marshal(data)
	var req struct {
		To       string `json:"to"`
		FilePath string `json:"filePath"`
	}
	if err := json.Unmarshal(jsonData, &req); err != nil {
		return
	}

	// Web客户端无法通过FilePath发送文件，此功能仅桌面端可用
	a.mu.RLock()
	client, clientExists := a.wsClients[conn]
	a.mu.RUnlock()

	if clientExists && client.isDesktop {
		go a.network.SendFile(req.To, req.FilePath, fmt.Sprintf("%d", time.Now().UnixNano()))
	}
}

// handleWSClearUnread 清除未读
func (a *API) handleWSClearUnread(data interface{}) {
	jsonData, _ := json.Marshal(data)
	var req struct {
		DeviceID string `json:"deviceId"`
	}
	if err := json.Unmarshal(jsonData, &req); err != nil {
		return
	}
	a.discovery.ClearUnread(req.DeviceID)
}

// OnFileProgress 处理文件传输进度
func (a *API) OnFileProgress(fileID string, received int64, total int64) {
	// 如果total为0，尝试从pendingFiles获取
	if total <= 0 {
		pf := a.network.GetPendingFile(fileID)
		if pf != nil && pf.FileSize > 0 {
			total = pf.FileSize
		}
	}

	progress := map[string]interface{}{
		"fileId":   fileID,
		"received": received,
		"total":    total,
	}

	a.broadcastWS(WSMessage{Type: "fileProgress", Data: progress})
}

// OnIncomingMessage 处理收到的消息（来自QUIC）
func (a *API) OnIncomingMessage(msg *Message) {
	record := &ChatRecord{
		ID:       msg.ID,
		From:     msg.From,
		FromName: msg.FromName,
		To:       msg.To,
		Type:     string(msg.Type),
		Content:  msg.Content,
		Time:     msg.Time,
		Status:   "delivered",
	}

	if msg.Type == MsgFileOffer {
		record.Type = "file"
		record.FileID = msg.FileID
		record.FileName = msg.Content
		record.FileSize = msg.FileSize
		record.Status = "receiving"
	}

	if msg.Type == MsgFileDone {
		// 文件传输完成 - 由 handleFileDone 处理重命名和通知
		return
	}

	a.addHistory(msg.From, record)
	a.discovery.IncUnread(msg.From)

	// 检查消息是否发给某个Web客户端
	if msg.To != "" && a.discovery.IsWebDevice(msg.To) {
		// 路由到指定的Web客户端
		a.sendToWebDevice(msg.To, WSMessage{Type: "newMessage", Data: record})
		// 同时通知桌面客户端
		a.broadcastWS(WSMessage{Type: "newMessage", Data: record})
	} else {
		// 广播给所有WS客户端
		a.broadcastWS(WSMessage{Type: "newMessage", Data: record})
	}
}

// OnFileComplete 处理文件传输完成
func (a *API) OnFileComplete(fileID string, pf *PendingFile, finalPath string) {
	// 构造下载URL
	fileName := ""
	if pf != nil {
		fileName = pf.FileName
	}
	if fileName == "" {
		fileName = filepath.Base(finalPath)
	}
	fileUrl := "/api/files/" + fileName

	record := &ChatRecord{
		ID:       fmt.Sprintf("%s-done", fileID),
		From:     "",
		FromName: "",
		Type:     "file",
		Content:  fileName,
		Time:     time.Now().UnixMilli(),
		FileID:   fileID,
		FileName: fileName,
		FileUrl:  fileUrl,
		Status:   "delivered",
	}

	if pf != nil {
		record.From = pf.FromID
		record.FromName = pf.FromName
		record.To = pf.ToID
		record.FileSize = pf.FileSize
	}

	// 更新历史记录中对应的file offer记录，添加下载URL和更新状态
	if record.From != "" {
		a.mu.Lock()
		if records, ok := a.chatHistory[record.From]; ok {
			for _, r := range records {
				if r.FileID == fileID && r.Status == "receiving" {
					r.Status = "delivered"
					r.FileUrl = fileUrl
					break
				}
			}
		}
		a.mu.Unlock()
	}

	if record.From == "" {
		log.Printf("[API] 文件完成但找不到发送者信息: %s", fileID)
		return
	}

	a.addHistory(record.From, record)
	a.discovery.IncUnread(record.From)

	// 通知所有客户端
	wsMsg := WSMessage{Type: "newMessage", Data: record}

	if record.To != "" && a.discovery.IsWebDevice(record.To) {
		a.sendToWebDevice(record.To, wsMsg)
		a.broadcastWS(WSMessage{Type: "newMessage", Data: record})
	} else {
		a.broadcastWS(wsMsg)
	}

	// 也发送fileDone事件（向后兼容）
	a.broadcastWS(WSMessage{Type: "fileDone", Data: map[string]string{
		"fileId":   fileID,
		"fileName": fileName,
		"fileUrl":  fileUrl,
	}})
}

// addHistory 添加聊天记录
func (a *API) addHistory(deviceID string, record *ChatRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.chatHistory[deviceID] == nil {
		a.chatHistory[deviceID] = make([]*ChatRecord, 0)
	}
	a.chatHistory[deviceID] = append(a.chatHistory[deviceID], record)
}

// broadcastWS 广播WebSocket消息
func (a *API) broadcastWS(msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for conn := range a.wsClients {
		conn.WriteMessage(websocket.TextMessage, data)
	}
}

// sendWS 发送WebSocket消息
func (a *API) sendWS(conn *websocket.Conn, msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	conn.WriteMessage(websocket.TextMessage, data)
}

// OnDevicesChanged 设备变化通知
func (a *API) OnDevicesChanged() {
	a.broadcastWS(WSMessage{
		Type: "devices",
		Data: a.discovery.GetDevices(),
	})
}

// disconnectBrowserClients 断开所有浏览器WS连接
func (a *API) disconnectBrowserClients() {
	a.mu.Lock()
	var browserConns []*websocket.Conn
	var webDeviceIDs []string
	for conn, client := range a.wsClients {
		if !client.isDesktop {
			browserConns = append(browserConns, conn)
			webDeviceIDs = append(webDeviceIDs, client.deviceID)
			delete(a.wsClients, conn)
		}
	}
	a.mu.Unlock()

	// 在锁外移除Web设备和关闭连接
	for _, id := range webDeviceIDs {
		a.discovery.RemoveWebDevice(id)
	}
	for _, conn := range browserConns {
		conn.Close()
	}
}