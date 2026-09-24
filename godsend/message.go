package godsend

import "encoding/json"

// MessageType 消息类型
type MessageType string

const (
	MsgText       MessageType = "text"        // 文本消息
	MsgImage      MessageType = "image"       // 图片消息
	MsgFileOffer  MessageType = "file_offer"  // 文件传输请求
	MsgFileAccept MessageType = "file_accept" // 接受文件传输
	MsgFileReject MessageType = "file_reject" // 拒绝文件传输
	MsgFileData   MessageType = "file_data"   // 文件数据块
	MsgFileDone   MessageType = "file_done"   // 文件传输完成
	MsgTyping     MessageType = "typing"      // 正在输入
	MsgRead       MessageType = "read"        // 已读回执
)

// Message 聊天消息
type Message struct {
	ID       string      `json:"id"`
	Type     MessageType `json:"type"`
	From     string      `json:"from"`     // 发送者设备ID
	FromName string      `json:"fromName"` // 发送者设备名
	To       string      `json:"to"`       // 接收者设备ID
	Content  string      `json:"content"`  // 文本内容或文件名
	Time     int64       `json:"time"`     // 时间戳
	// 文件传输相关
	FileID   string `json:"fileId,omitempty"`   // 文件传输ID
	FileSize int64  `json:"fileSize,omitempty"` // 文件大小
	FileType string `json:"fileType,omitempty"` // 文件MIME类型
	ChunkSeq int    `json:"chunkSeq,omitempty"` // 数据块序号
	ChunkLen int    `json:"chunkLen,omitempty"` // 数据块长度
	Data     []byte `json:"data,omitempty"`     // 二进制数据
}

// Encode 编码消息为JSON
func (m *Message) Encode() ([]byte, error) {
	return json.Marshal(m)
}

// DecodeMessage 从JSON解码消息
func DecodeMessage(data []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// DiscoveryPacket 设备发现广播包
type DiscoveryPacket struct {
	DeviceID      string `json:"deviceId"`
	DeviceName    string `json:"deviceName"`
	IP            string `json:"ip"`
	Port          int    `json:"port"`
	Action        string `json:"action"`                  // "online", "offline", "heartbeat", "webJoin", "webLeave"
	WebDeviceID   string `json:"webDeviceId,omitempty"`   // web设备ID（webJoin/webLeave时使用）
	WebDeviceName string `json:"webDeviceName,omitempty"` // web设备名称
	WebDeviceIP   string `json:"webDeviceIp,omitempty"`   // web设备IP
}

// Encode 编码发现包
func (d *DiscoveryPacket) Encode() ([]byte, error) {
	return json.Marshal(d)
}

// DecodeDiscoveryPacket 解码发现包
func DecodeDiscoveryPacket(data []byte) (*DiscoveryPacket, error) {
	var pkt DiscoveryPacket
	if err := json.Unmarshal(data, &pkt); err != nil {
		return nil, err
	}
	return &pkt, nil
}

// WSMessage WebSocket消息(前端与后端通信)
type WSMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// ChatRecord 聊天记录(用于前端展示)
type ChatRecord struct {
	ID       string `json:"id"`
	From     string `json:"from"`
	FromName string `json:"fromName"`
	To       string `json:"to"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	Time     int64  `json:"time"`
	FileID   string `json:"fileId,omitempty"`
	FileName string `json:"fileName,omitempty"`
	FileSize int64  `json:"fileSize,omitempty"`
	FileType string `json:"fileType,omitempty"`
	FileUrl  string `json:"fileUrl,omitempty"`  // 文件下载URL（网页端使用）
	Status   string `json:"status,omitempty"`   // "sending", "sent", "delivered", "failed", "receiving"
}