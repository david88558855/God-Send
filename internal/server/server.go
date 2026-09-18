package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shenchuan/shenchuan/internal/config"
	"github.com/shenchuan/shenchuan/internal/discovery"
	"github.com/shenchuan/shenchuan/internal/protocol"
	"github.com/shenchuan/shenchuan/internal/transfer"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有来源(局域网使用)
	},
	ReadBufferSize:  1024 * 1024, // 1MB
	WriteBufferSize: 1024 * 1024,
}

// Server HTTP/WebSocket服务器
type Server struct {
	config    *config.Config
	discovery *discovery.Discovery
	transfer  *transfer.Manager
	clients   map[string]*ClientConn
	clientsMu sync.RWMutex
	messages  []*protocol.Message
	msgMu     sync.RWMutex
	server    *http.Server
}

// ClientConn 客户端连接
type ClientConn struct {
	ID     string
	Name   string
	Color  string
	Conn   *websocket.Conn
	mu     sync.Mutex
}

// New 创建服务器
func New(cfg *config.Config, disc *discovery.Discovery, mgr *transfer.Manager) *Server {
	return &Server{
		config:    cfg,
		discovery: disc,
		transfer:  mgr,
		clients:   make(map[string]*ClientConn),
		messages:  make([]*protocol.Message, 0),
	}
}

// Start 启动服务器
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// WebSocket端点
	mux.HandleFunc(config.DefaultWebSocketPath, s.handleWebSocket)

	// API端点
	apiPrefix := config.DefaultAPIPath
	mux.HandleFunc(apiPrefix+"/info", s.handleInfo)
	mux.HandleFunc(apiPrefix+"/peers", s.handlePeers)
	mux.HandleFunc(apiPrefix+"/messages", s.handleMessages)
	mux.HandleFunc(apiPrefix+"/transfers", s.handleTransfers)

	// 文件上传
	mux.HandleFunc(apiPrefix+"/upload", s.handleUpload)

	// 文件下载
	mux.HandleFunc(apiPrefix+"/download/", s.handleDownload)

	// 静态文件服务(前端)
	mux.HandleFunc("/", s.handleStatic)

	addr := fmt.Sprintf(":%d", s.config.Port)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  0, // 文件上传需要长超时
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("[服务器] 启动于端口 %d", s.config.Port)

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[服务器] 启动失败: %v", err)
		}
	}()

	return nil
}

// Stop 停止服务器
func (s *Server) Stop() {
	if s.server != nil {
		s.server.Close()
	}

	// 关闭所有WebSocket连接
	s.clientsMu.Lock()
	for _, client := range s.clients {
		client.Conn.Close()
	}
	s.clientsMu.Unlock()

	log.Println("[服务器] 已停止")
}

// handleWebSocket 处理WebSocket连接
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WebSocket] 升级失败: %v", err)
		return
	}
	defer conn.Close()

	// 生成客户端ID
	clientID := generateClientID(conn.RemoteAddr().String())
	client := &ClientConn{
		ID:    clientID,
		Name:  "未知用户",
		Color: randomColor(),
		Conn:  conn,
	}

	// 注册客户端
	s.clientsMu.Lock()
	s.clients[clientID] = client
	s.clientsMu.Unlock()

	log.Printf("[WebSocket] 新连接: %s", clientID)

	// 发送历史消息
	s.msgMu.RLock()
	for _, msg := range s.messages {
		data, _ := json.Marshal(msg)
		conn.WriteMessage(websocket.TextMessage, data)
	}
	s.msgMu.RUnlock()

	// 广播设备上线通知
	s.broadcastDeviceAnnounce(client)

	// 消息循环
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[WebSocket] 读取错误: %v", err)
			}
			break
		}

		s.handleMessage(client, message)
	}

	// 移除客户端
	s.clientsMu.Lock()
	delete(s.clients, clientID)
	s.clientsMu.Unlock()

	// 广播设备离线通知
	s.broadcastDeviceLeave(client)

	log.Printf("[WebSocket] 连接关闭: %s", clientID)
}

// handleMessage 处理WebSocket消息
func (s *Server) handleMessage(client *ClientConn, data []byte) {
	var msg protocol.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("[WebSocket] 消息解析失败: %v", err)
		return
	}

	// 填充发送者信息
	msg.SenderID = client.ID
	msg.SenderName = client.Name
	msg.SenderColor = client.Color
	msg.Timestamp = time.Now()

	if msg.ID == "" {
		msg.ID = generateMessageID()
	}

	// 根据消息类型处理
	switch msg.Type {
	case protocol.TypeText:
		s.storeAndBroadcast(&msg)

	case protocol.TypeImage, protocol.TypeFile:
		s.storeAndBroadcast(&msg)

	case protocol.TypeFileAck:
		s.handleFileAck(&msg)

	case protocol.TypeTyping:
		s.broadcast(&msg)

	case protocol.TypeDeviceAnnounce:
		// 更新客户端信息
		var payload protocol.DevicePayload
		if b, err := json.Marshal(msg.Payload); err == nil {
			json.Unmarshal(b, &payload)
			client.Name = payload.DeviceName
			client.Color = payload.AvatarColor
		}

	default:
		log.Printf("[WebSocket] 未知消息类型: %s", msg.Type)
	}
}

// handleFileAck 处理文件确认
func (s *Server) handleFileAck(msg *protocol.Message) {
	var payload protocol.FileAckPayload
	if b, err := json.Marshal(msg.Payload); err == nil {
		json.Unmarshal(b, &payload)
	}

	if payload.Accepted {
		s.transfer.UpdateState(payload.FileID, protocol.StateAccepting)
	} else {
		s.transfer.UpdateState(payload.FileID, protocol.StateRejected)
	}

	s.broadcast(msg)
}

// storeAndBroadcast 存储并广播消息
func (s *Server) storeAndBroadcast(msg *protocol.Message) {
	// 存储消息(保留最近1000条)
	s.msgMu.Lock()
	s.messages = append(s.messages, msg)
	if len(s.messages) > 1000 {
		s.messages = s.messages[len(s.messages)-1000:]
	}
	s.msgMu.Unlock()

	// 广播给所有客户端
	s.broadcast(msg)
}

// broadcast 广播消息给所有客户端
func (s *Server) broadcast(msg *protocol.Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[WebSocket] 消息编码失败: %v", err)
		return
	}

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for _, client := range s.clients {
		client.mu.Lock()
		err := client.Conn.WriteMessage(websocket.TextMessage, data)
		client.mu.Unlock()
		if err != nil {
			log.Printf("[WebSocket] 发送失败: %v", err)
		}
	}
}

// broadcastDeviceAnnounce 广播设备上线
func (s *Server) broadcastDeviceAnnounce(client *ClientConn) {
	msg := &protocol.Message{
		ID:          generateMessageID(),
		Type:        protocol.TypeDeviceAnnounce,
		SenderID:    client.ID,
		SenderName:  client.Name,
		SenderColor: client.Color,
		Timestamp:   time.Now(),
		Payload: protocol.DevicePayload{
			DeviceID:    client.ID,
			DeviceName:  client.Name,
			AvatarColor: client.Color,
		},
	}
	s.broadcast(msg)
}

// broadcastDeviceLeave 广播设备离线
func (s *Server) broadcastDeviceLeave(client *ClientConn) {
	msg := &protocol.Message{
		ID:          generateMessageID(),
		Type:        protocol.TypeDeviceLeave,
		SenderID:    client.ID,
		SenderName:  client.Name,
		SenderColor: client.Color,
		Timestamp:   time.Now(),
		Payload: protocol.DevicePayload{
			DeviceID: client.ID,
		},
	}
	s.broadcast(msg)
}

// handleInfo 处理信息请求
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]interface{}{
		"app_name":    config.AppName,
		"version":     config.Version,
		"device_id":   s.discovery.GetDeviceID(),
		"device_name": s.config.DeviceName,
		"port":        s.config.Port,
		"color":       s.config.AvatarColor,
	}
	writeJSON(w, info)
}

// handlePeers 处理设备列表请求
func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	peers := s.discovery.GetPeers()
	writeJSON(w, peers)
}

// handleMessages 处理消息列表请求
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	s.msgMu.RLock()
	messages := make([]*protocol.Message, len(s.messages))
	copy(messages, s.messages)
	s.msgMu.RUnlock()
	writeJSON(w, messages)
}

// handleTransfers 处理传输列表请求
func (s *Server) handleTransfers(w http.ResponseWriter, r *http.Request) {
	transfers := s.transfer.GetTransfers()
	writeJSON(w, transfers)
}

// handleUpload 处理文件上传
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	// 限制上传大小(10GB)
	r.Body = http.MaxBytesReader(w, r.Body, config.MaxFileSize)

	// 解析multipart表单
	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB缓冲
		http.Error(w, fmt.Sprintf("解析表单失败: %v", err), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf("获取文件失败: %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	senderID := r.FormValue("sender_id")
	senderName := r.FormValue("sender_name")
	fileID := r.FormValue("file_id")

	if fileID == "" {
		fileID = generateFileID(header.Filename, senderID)
	}

	// 保存文件
	savePath, err := s.transfer.SaveFile(fileID, file, header.Filename, header.Size)
	if err != nil {
		http.Error(w, fmt.Sprintf("保存文件失败: %v", err), http.StatusInternalServerError)
		return
	}

	// 广播文件消息
	mimeType := header.Header.Get("Content-Type")
	msgType := protocol.TypeFile
	if strings.HasPrefix(mimeType, "image/") {
		msgType = protocol.TypeImage
	}

	msg := &protocol.Message{
		ID:          generateMessageID(),
		Type:        msgType,
		SenderID:    senderID,
		SenderName:  senderName,
		SenderColor: r.FormValue("sender_color"),
		Timestamp:   time.Now(),
		Payload: map[string]interface{}{
			"file_id":   fileID,
			"file_name": header.Filename,
			"file_size": header.Size,
			"mime_type": mimeType,
			"save_path": savePath,
			"download_url": fmt.Sprintf("/api/download/%s", fileID),
		},
	}

	s.storeAndBroadcast(msg)

	// 返回结果
	writeJSON(w, map[string]interface{}{
		"success":    true,
		"file_id":    fileID,
		"file_name":  header.Filename,
		"file_size":  header.Size,
		"save_path":  savePath,
	})
}

// handleDownload 处理文件下载
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	// 从URL路径提取文件ID
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.Error(w, "无效的文件ID", http.StatusBadRequest)
		return
	}
	fileID := parts[len(parts)-1]

	transferInfo, ok := s.transfer.GetTransfer(fileID)
	if !ok {
		http.Error(w, "文件未找到", http.StatusNotFound)
		return
	}

	if transferInfo.FilePath == "" {
		http.Error(w, "文件路径无效", http.StatusNotFound)
		return
	}

	s.transfer.ServeFile(transferInfo.FilePath, transferInfo.FileName).ServeHTTP(w, r)
}

// handleStatic 处理静态文件
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	// 前端静态文件目录
	staticDir := "frontend/dist"
	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		// 如果没有前端构建，返回API信息
		writeJSON(w, map[string]interface{}{
			"app":     config.AppName,
			"version": config.Version,
			"status":  "running",
		})
		return
	}

	// 尝试提供静态文件
	filePath := filepath.Join(staticDir, r.URL.Path)
	if r.URL.Path == "/" || r.URL.Path == "" {
		filePath = filepath.Join(staticDir, "index.html")
	}

	// 安全检查
	absPath, _ := filepath.Abs(filePath)
	absStatic, _ := filepath.Abs(staticDir)
	if !strings.HasPrefix(absPath, absStatic) {
		http.Error(w, "禁止访问", http.StatusForbidden)
		return
	}

	http.ServeFile(w, r, filePath)
}

// writeJSON 写入JSON响应
func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(data)
}

// generateClientID 生成客户端ID
func generateClientID(addr string) string {
	return fmt.Sprintf("client_%d", time.Now().UnixNano())
}

// generateMessageID 生成消息ID
func generateMessageID() string {
	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}

// generateFileID 生成文件ID
func generateFileID(name, sender string) string {
	return fmt.Sprintf("file_%d", time.Now().UnixNano())
}

// randomColor 生成随机颜色
func randomColor() string {
	colors := []string{
		"#F44336", "#E91E63", "#9C27B0", "#673AB7",
		"#3F51B5", "#2196F3", "#03A9F4", "#00BCD4",
		"#009688", "#4CAF50", "#8BC34A", "#CDDC39",
		"#FFC107", "#FF9800", "#FF5722", "#795548",
	}
	return colors[time.Now().UnixNano()%int64(len(colors))]
}

// init 确保io接口被使用
var _ = io.EOF