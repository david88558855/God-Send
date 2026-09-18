package protocol

import "time"

// MessageType 消息类型
type MessageType string

const (
	// TypeText 文本消息
	TypeText MessageType = "text"
	// TypeImage 图片消息
	TypeImage MessageType = "image"
	// TypeFile 文件消息
	TypeFile MessageType = "file"
	// TypeFileChunk 文件分块数据
	TypeFileChunk MessageType = "file_chunk"
	// TypeFileAck 文件确认
	TypeFileAck MessageType = "file_ack"
	// TypeDeviceAnnounce 设备公告
	TypeDeviceAnnounce MessageType = "device_announce"
	// TypeDeviceLeave 设备离开
	TypeDeviceLeave MessageType = "device_leave"
	// TypeTyping 正在输入
	TypeTyping MessageType = "typing"
	// TypeProgress 传输进度
	TypeProgress MessageType = "progress"
)

// Message 基础消息结构
type Message struct {
	// ID 消息唯一ID
	ID string `json:"id"`
	// Type 消息类型
	Type MessageType `json:"type"`
	// SenderID 发送者ID
	SenderID string `json:"sender_id"`
	// SenderName 发送者名称
	SenderName string `json:"sender_name"`
	// SenderColor 发送者颜色
	SenderColor string `json:"sender_color"`
	// Timestamp 时间戳
	Timestamp time.Time `json:"timestamp"`
	// Payload 消息内容
	Payload interface{} `json:"payload"`
}

// TextPayload 文本消息内容
type TextPayload struct {
	Content string `json:"content"`
}

// ImagePayload 图片消息内容
type ImagePayload struct {
	// FileID 文件ID
	FileID string `json:"file_id"`
	// FileName 文件名
	FileName string `json:"file_name"`
	// FileSize 文件大小
	FileSize int64 `json:"file_size"`
	// Width 图片宽度
	Width int `json:"width,omitempty"`
	// Height 图片高度
	Height int `json:"height,omitempty"`
	// ThumbnailURL 缩略图URL
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
	// DownloadURL 下载URL
	DownloadURL string `json:"download_url"`
}

// FilePayload 文件消息内容
type FilePayload struct {
	// FileID 文件ID
	FileID string `json:"file_id"`
	// FileName 文件名
	FileName string `json:"file_name"`
	// FileSize 文件大小
	FileSize int64 `json:"file_size"`
	// MIMEType MIME类型
	MIMEType string `json:"mime_type"`
	// Checksum 文件校验和(SHA256)
	Checksum string `json:"checksum,omitempty"`
	// DownloadURL 下载URL
	DownloadURL string `json:"download_url"`
}

// FileChunkPayload 文件分块内容
type FileChunkPayload struct {
	// FileID 文件ID
	FileID string `json:"file_id"`
	// ChunkIndex 分块索引
	ChunkIndex int `json:"chunk_index"`
	// TotalChunks 总分块数
	TotalChunks int `json:"total_chunks"`
	// Data 分块数据(Base64)
	Data string `json:"data"`
}

// FileAckPayload 文件确认内容
type FileAckPayload struct {
	// FileID 文件ID
	FileID string `json:"file_id"`
	// Accepted 是否接受
	Accepted bool `json:"accepted"`
	// SavePath 保存路径
	SavePath string `json:"save_path,omitempty"`
}

// DevicePayload 设备信息内容
type DevicePayload struct {
	// DeviceID 设备ID
	DeviceID string `json:"device_id"`
	// DeviceName 设备名称
	DeviceName string `json:"device_name"`
	// IPAddress IP地址
	IPAddress string `json:"ip_address"`
	// Port 端口
	Port int `json:"port"`
	// Platform 平台
	Platform string `json:"platform"`
	// AvatarColor 头像颜色
	AvatarColor string `json:"avatar_color"`
}

// ProgressPayload 传输进度内容
type ProgressPayload struct {
	// FileID 文件ID
	FileID string `json:"file_id"`
	// FileName 文件名
	FileName string `json:"file_name"`
	// BytesTransferred 已传输字节数
	BytesTransferred int64 `json:"bytes_transferred"`
	// TotalBytes 总字节数
	TotalBytes int64 `json:"total_bytes"`
	// Percentage 百分比
	Percentage float64 `json:"percentage"`
	// Speed 传输速度(bytes/s)
	Speed float64 `json:"speed"`
	// State 状态: uploading, downloading, completed, error
	State string `json:"state"`
}

// TypingPayload 正在输入内容
type TypingPayload struct {
	// IsTyping 是否正在输入
	IsTyping bool `json:"is_typing"`
}

// DeviceInfo 设备信息
type DeviceInfo struct {
	// ID 设备ID
	ID string `json:"id"`
	// Name 设备名称
	Name string `json:"name"`
	// IPAddress IP地址
	IPAddress string `json:"ip_address"`
	// Port 端口
	Port int `json:"port"`
	// Platform 平台
	Platform string `json:"platform"`
	// AvatarColor 头像颜色
	AvatarColor string `json:"avatar_color"`
	// LastSeen 最后可见时间
	LastSeen time.Time `json:"last_seen"`
}

// TransferInfo 传输信息
type TransferInfo struct {
	// FileID 文件ID
	FileID string `json:"file_id"`
	// FileName 文件名
	FileName string `json:"file_name"`
	// FilePath 文件路径
	FilePath string `json:"file_path"`
	// FileSize 文件大小
	FileSize int64 `json:"file_size"`
	// MIMEType MIME类型
	MIMEType string `json:"mime_type"`
	// SenderID 发送者ID
	SenderID string `json:"sender_id"`
	// SenderName 发送者名称
	SenderName string `json:"sender_name"`
	// State 状态
	State TransferState `json:"state"`
	// Progress 进度
	Progress float64 `json:"progress"`
	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`
}

// TransferState 传输状态
type TransferState string

const (
	// StatePending 等待中
	StatePending TransferState = "pending"
	// StateAccepting 接受中
	StateAccepting TransferState = "accepting"
	// StateTransferring 传输中
	StateTransferring TransferState = "transferring"
	// StateCompleted 已完成
	StateCompleted TransferState = "completed"
	// StateError 错误
	StateError TransferState = "error"
	// StateCancelled 已取消
	StateCancelled TransferState = "cancelled"
	// StateRejected 已拒绝
	StateRejected TransferState = "rejected"
)