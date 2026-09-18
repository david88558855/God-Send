package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/God-Send/God-Send/internal/config"
	"github.com/God-Send/God-Send/internal/protocol"
)

// Manager 传输管理器
type Manager struct {
	config     *config.Config
	transfers  map[string]*protocol.TransferInfo
	transfersMu sync.RWMutex
	onUpdate   func(*protocol.TransferInfo)
}

// NewManager 创建传输管理器
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		config:    cfg,
		transfers: make(map[string]*protocol.TransferInfo),
	}
}

// SetOnUpdate 设置更新回调
func (m *Manager) SetOnUpdate(fn func(*protocol.TransferInfo)) {
	m.onUpdate = fn
}

// CreateTransfer 创建传输
func (m *Manager) CreateTransfer(fileName, filePath string, fileSize int64, mimeType, senderID, senderName string) *protocol.TransferInfo {
	fileID := generateFileID(fileName, senderID)

	transfer := &protocol.TransferInfo{
		FileID:     fileID,
		FileName:   fileName,
		FilePath:   filePath,
		FileSize:   fileSize,
		MIMEType:   mimeType,
		SenderID:   senderID,
		SenderName: senderName,
		State:      protocol.StatePending,
		Progress:   0,
		CreatedAt:  time.Now(),
	}

	m.transfersMu.Lock()
	m.transfers[fileID] = transfer
	m.transfersMu.Unlock()

	return transfer
}

// GetTransfer 获取传输信息
func (m *Manager) GetTransfer(fileID string) (*protocol.TransferInfo, bool) {
	m.transfersMu.RLock()
	defer m.transfersMu.RUnlock()
	t, ok := m.transfers[fileID]
	return t, ok
}

// UpdateState 更新传输状态
func (m *Manager) UpdateState(fileID string, state protocol.TransferState) {
	m.transfersMu.Lock()
	defer m.transfersMu.Unlock()

	if t, ok := m.transfers[fileID]; ok {
		t.State = state
		if m.onUpdate != nil {
			go m.onUpdate(t)
		}
	}
}

// UpdateProgress 更新传输进度
func (m *Manager) UpdateProgress(fileID string, progress float64) {
	m.transfersMu.Lock()
	defer m.transfersMu.Unlock()

	if t, ok := m.transfers[fileID]; ok {
		t.Progress = progress
		if m.onUpdate != nil {
			go m.onUpdate(t)
		}
	}
}

// SaveFile 保存上传的文件
func (m *Manager) SaveFile(fileID string, reader io.Reader, fileName string, fileSize int64) (string, error) {
	// 确保下载目录存在
	if err := os.MkdirAll(m.config.DownloadDir, 0755); err != nil {
		return "", fmt.Errorf("创建下载目录失败: %w", err)
	}

	// 生成安全文件名
	safeName := sanitizeFileName(fileName)
	dstPath := filepath.Join(m.config.DownloadDir, safeName)

	// 如果文件已存在，添加序号
	counter := 1
	for {
		if _, err := os.Stat(dstPath); os.IsNotExist(err) {
			break
		}
		ext := filepath.Ext(safeName)
		base := strings.TrimSuffix(safeName, ext)
		dstPath = filepath.Join(m.config.DownloadDir, fmt.Sprintf("%s_%d%s", base, counter, ext))
		counter++
	}

	// 创建目标文件
	dst, err := os.Create(dstPath)
	if err != nil {
		return "", fmt.Errorf("创建文件失败: %w", err)
	}
	defer dst.Close()

	// 写入文件
	written, err := io.Copy(dst, reader)
	if err != nil {
		os.Remove(dstPath)
		return "", fmt.Errorf("写入文件失败: %w", err)
	}

	if written != fileSize && fileSize > 0 {
		os.Remove(dstPath)
		return "", fmt.Errorf("文件大小不匹配: 期望 %d, 实际 %d", fileSize, written)
	}

	log.Printf("[传输] 文件已保存: %s (%s)", safeName, formatSize(written))

	// 更新传输状态
	m.transfersMu.Lock()
	if t, ok := m.transfers[fileID]; ok {
		t.State = protocol.StateCompleted
		t.Progress = 100
		t.FilePath = dstPath
		if m.onUpdate != nil {
			go m.onUpdate(t)
		}
	}
	m.transfersMu.Unlock()

	return dstPath, nil
}

// ServeFile 提供文件下载服务
func (m *Manager) ServeFile(filePath string, fileName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file, err := os.Open(filePath)
		if err != nil {
			http.Error(w, "文件未找到", http.StatusNotFound)
			return
		}
		defer file.Close()

		stat, err := file.Stat()
		if err != nil {
			http.Error(w, "文件状态错误", http.StatusInternalServerError)
			return
		}

		// 设置响应头
		mimeType := mime.TypeByExtension(filepath.Ext(fileName))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))

		http.ServeContent(w, r, fileName, stat.ModTime(), file)
	}
}

// GetTransfers 获取所有传输
func (m *Manager) GetTransfers() []*protocol.TransferInfo {
	m.transfersMu.RLock()
	defer m.transfersMu.RUnlock()

	result := make([]*protocol.TransferInfo, 0, len(m.transfers))
	for _, t := range m.transfers {
		result = append(result, t)
	}
	return result
}

// DeleteTransfer 删除传输记录
func (m *Manager) DeleteTransfer(fileID string) {
	m.transfersMu.Lock()
	defer m.transfersMu.Unlock()
	delete(m.transfers, fileID)
}

// generateFileID 生成文件ID
func generateFileID(fileName, senderID string) string {
	data := fmt.Sprintf("%s_%s_%d", fileName, senderID, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:16])
}

// sanitizeFileName 清理文件名
func sanitizeFileName(name string) string {
	// 移除路径分隔符
	name = filepath.Base(name)
	// 替换不安全字符
	unsafe := []string{"\\", ":", "*", "?", "\"", "<", ">", "|", "\x00"}
	for _, ch := range unsafe {
		name = strings.ReplaceAll(name, ch, "_")
	}
	if name == "" || name == "." || name == ".." {
		name = fmt.Sprintf("file_%d", time.Now().Unix())
	}
	return name
}

// formatSize 格式化文件大小
func formatSize(bytes int64) string {
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