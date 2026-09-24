package godsend

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

// PendingFile 待完成的文件传输信息
type PendingFile struct {
	FileName      string
	FileSize      int64
	FromID        string
	FromName      string
	ToID          string
	ReceivedBytes int64 // 已接收字节数
}

// Network QUIC网络管理
type Network struct {
	mu           sync.RWMutex
	config       *Config
	discovery    *Discovery
	listener     *quic.Listener
	connections  map[string]*quic.Conn // deviceID -> connection
	pendingFiles map[string]*PendingFile // fileID -> 文件传输信息
	onMessage    func(*Message)
	onFileProgress func(fileID string, received int64, total int64)
	onFileComplete func(fileID string, pf *PendingFile, finalPath string)
	stopCh       chan struct{}
	tlsConfig    *tls.Config
}

// NewNetwork 创建网络管理
func NewNetwork(cfg *Config, disc *Discovery) *Network {
	return &Network{
		config:       cfg,
		discovery:    disc,
		connections:  make(map[string]*quic.Conn),
		pendingFiles: make(map[string]*PendingFile),
		stopCh:       make(chan struct{}),
	}
}

// SetOnMessage 设置消息回调
func (n *Network) SetOnMessage(fn func(*Message)) {
	n.onMessage = fn
}

// SetOnFileProgress 设置文件进度回调
func (n *Network) SetOnFileProgress(fn func(string, int64, int64)) {
	n.onFileProgress = fn
}

// SetOnFileComplete 设置文件完成回调
func (n *Network) SetOnFileComplete(fn func(string, *PendingFile, string)) {
	n.onFileComplete = fn
}

// Start 启动QUIC监听
func (n *Network) Start() error {
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{n.generateSelfSignedCert()},
		NextProtos:   []string{"god-send"},
		InsecureSkipVerify: true,
	}
	n.tlsConfig = tlsConfig

	port := n.config.GetPort()
	addr := fmt.Sprintf(":%d", port)
	listener, err := quic.ListenAddr(addr, tlsConfig, &quic.Config{
		KeepAlivePeriod: 5 * time.Second,
		MaxIdleTimeout:  30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("QUIC监听失败: %v", err)
	}
	n.listener = listener

	go n.acceptLoop()
	log.Printf("[网络] QUIC监听已启动, 端口: %d", port)
	return nil
}

// Stop 停止网络
func (n *Network) Stop() {
	close(n.stopCh)
	if n.listener != nil {
		n.listener.Close()
	}
	n.mu.Lock()
	for _, conn := range n.connections {
		conn.CloseWithError(0, "shutdown")
	}
	n.connections = make(map[string]*quic.Conn)
	n.mu.Unlock()
}

// acceptLoop 接受连接
func (n *Network) acceptLoop() {
	for {
		select {
		case <-n.stopCh:
			return
		default:
		}
		conn, err := n.listener.Accept(context.Background())
		if err != nil {
			continue
		}
		go n.handleConnection(conn)
	}
}

// handleConnection 处理连接
func (n *Network) handleConnection(conn *quic.Conn) {
	for {
		select {
		case <-n.stopCh:
			return
		default:
		}
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			conn.CloseWithError(0, "stream error")
			return
		}
		go n.handleStream(stream, conn)
	}
}

// handleStream 处理数据流
func (n *Network) handleStream(stream *quic.Stream, conn *quic.Conn) {
	defer stream.Close()

	// 读取消息长度
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(stream, lenBuf); err != nil {
		return
	}
	msgLen := binary.BigEndian.Uint32(lenBuf)
	if msgLen > 100*1024*1024 { // 最大100MB
		return
	}

	// 读取消息内容
	data := make([]byte, msgLen)
	if _, err := io.ReadFull(stream, data); err != nil {
		return
	}

	msg, err := DecodeMessage(data)
	if err != nil {
		return
	}

	// 存储连接
	n.mu.Lock()
	if msg.From != "" && msg.From != n.config.GetDeviceID() {
		if _, exists := n.connections[msg.From]; !exists {
			n.connections[msg.From] = conn
		}
	}
	n.mu.Unlock()

	// 处理文件数据
	if msg.Type == MsgFileData {
		n.handleFileData(msg)
		return
	}

	if msg.Type == MsgFileOffer {
		// 存储文件传输信息，用于完成时重命名
		n.mu.Lock()
		n.pendingFiles[msg.FileID] = &PendingFile{
			FileName: msg.Content,
			FileSize: msg.FileSize,
			FromID:   msg.From,
			FromName: msg.FromName,
			ToID:     msg.To,
		}
		n.mu.Unlock()
	}

	if msg.Type == MsgFileDone {
		n.handleFileDone(msg)
		return
	}

	// 回调消息
	if n.onMessage != nil {
		n.onMessage(msg)
	}
}

// SendMessage 发送消息
func (n *Network) SendMessage(msg *Message) error {
	msg.From = n.config.GetDeviceID()
	msg.FromName = n.config.GetDeviceName()
	msg.Time = time.Now().UnixMilli()

	conn, err := n.getConnection(msg.To)
	if err != nil {
		return err
	}

	return n.sendOnConnection(conn, msg)
}

// SendFileOffer 发送文件传输请求
func (n *Network) SendFileOffer(toDeviceID string, filePath string) error {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	msg := &Message{
		Type:     MsgFileOffer,
		From:     n.config.GetDeviceID(),
		FromName: n.config.GetDeviceName(),
		To:       toDeviceID,
		Content:  filepath.Base(filePath),
		Time:     time.Now().UnixMilli(),
		FileID:   fmt.Sprintf("%d", time.Now().UnixNano()),
		FileSize: fileInfo.Size(),
		FileType: filepath.Ext(filePath),
	}

	conn, err := n.getConnection(toDeviceID)
	if err != nil {
		return err
	}

	return n.sendOnConnection(conn, msg)
}

// SendFile 发送文件数据
func (n *Network) SendFile(toDeviceID string, filePath string, fileID string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	fileInfo, _ := file.Stat()
	totalSize := fileInfo.Size()
	var sent int64

	conn, err := n.getConnection(toDeviceID)
	if err != nil {
		return err
	}

	chunkSize := 32 * 1024 // 32KB chunks
	buf := make([]byte, chunkSize)
	seq := 0

	for {
		nr, err := file.Read(buf)
		if nr > 0 {
			msg := &Message{
				Type:     MsgFileData,
				From:     n.config.GetDeviceID(),
				FromName: n.config.GetDeviceName(),
				To:       toDeviceID,
				Time:     time.Now().UnixMilli(),
				FileID:   fileID,
				ChunkSeq: seq,
				Data:     buf[:nr],
			}
			if err2 := n.sendOnConnection(conn, msg); err2 != nil {
				return err2
			}
			sent += int64(nr)
			seq++
			if n.onFileProgress != nil {
				n.onFileProgress(fileID, sent, totalSize)
			}
		}
		if err != nil {
			break
		}
	}

	// 发送完成消息
	doneMsg := &Message{
		Type:    MsgFileDone,
		From:    n.config.GetDeviceID(),
		FromName: n.config.GetDeviceName(),
		To:      toDeviceID,
		Time:    time.Now().UnixMilli(),
		FileID:  fileID,
	}
	return n.sendOnConnection(conn, doneMsg)
}

// handleFileData 处理接收到的文件数据
func (n *Network) handleFileData(msg *Message) {
	savePath := n.config.GetSavePath()
	os.MkdirAll(savePath, 0755)

	// 文件数据写入临时文件
	tmpPath := filepath.Join(savePath, ".tmp_"+msg.FileID)
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(msg.Data)

	// 更新已接收字节数并报告进度
	n.mu.Lock()
	pf, exists := n.pendingFiles[msg.FileID]
	if exists {
		pf.ReceivedBytes += int64(len(msg.Data))
	}
	n.mu.Unlock()

	if n.onFileProgress != nil && exists {
		n.onFileProgress(msg.FileID, pf.ReceivedBytes, pf.FileSize)
	}
}

// handleFileDone 处理文件传输完成
func (n *Network) handleFileDone(msg *Message) {
	savePath := n.config.GetSavePath()

	n.mu.Lock()
	pf, exists := n.pendingFiles[msg.FileID]
	if exists {
		delete(n.pendingFiles, msg.FileID)
	}
	n.mu.Unlock()

	tmpPath := filepath.Join(savePath, ".tmp_"+msg.FileID)
	finalPath := ""

	if exists && pf.FileName != "" {
		// 重命名临时文件为实际文件名
		finalPath = filepath.Join(savePath, pf.FileName)
		// 如果目标文件已存在，添加序号
		baseName := pf.FileName
		ext := filepath.Ext(baseName)
		nameNoExt := baseName[:len(baseName)-len(ext)]
		counter := 1
		for {
			if _, err := os.Stat(finalPath); os.IsNotExist(err) {
				break
			}
			finalPath = filepath.Join(savePath, fmt.Sprintf("%s(%d)%s", nameNoExt, counter, ext))
			counter++
		}
		os.Rename(tmpPath, finalPath)
	} else {
		// 没有文件信息，尝试保留临时文件
		if _, err := os.Stat(tmpPath); err == nil {
			finalPath = tmpPath
		}
	}

	// 回调文件完成
	if n.onFileComplete != nil && finalPath != "" {
		n.onFileComplete(msg.FileID, pf, finalPath)
	}
}

// GetPendingFile 获取待完成文件信息
func (n *Network) GetPendingFile(fileID string) *PendingFile {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.pendingFiles[fileID]
}

// getConnection 获取或创建连接
func (n *Network) getConnection(deviceID string) (*quic.Conn, error) {
	n.mu.RLock()
	if conn, ok := n.connections[deviceID]; ok {
		n.mu.RUnlock()
		return conn, nil
	}
	n.mu.RUnlock()

	// 检查是否是跨桌面的relay web设备
	relayIP, relayPort, isRelay := n.discovery.GetDeviceRelay(deviceID)
	if isRelay {
		// 通过中继桌面转发，连接到中继桌面的QUIC端口
		addr := fmt.Sprintf("%s:%d", relayIP, relayPort)
		conn, err := quic.DialAddr(context.Background(), addr, n.tlsConfig, &quic.Config{
			KeepAlivePeriod: 5 * time.Second,
			MaxIdleTimeout:  30 * time.Second,
		})
		if err != nil {
			return nil, fmt.Errorf("连接中继设备失败: %v", err)
		}
		n.mu.Lock()
		n.connections[deviceID] = conn
		n.mu.Unlock()
		return conn, nil
	}

	// 通过设备发现获取地址
	dev := n.discovery.GetDevice(deviceID)
	if dev == nil {
		return nil, fmt.Errorf("设备不存在: %s", deviceID)
	}
	if !dev.Online {
		return nil, fmt.Errorf("设备离线: %s", dev.Name)
	}

	addr := fmt.Sprintf("%s:%d", dev.IP, dev.Port)

	conn, err := quic.DialAddr(context.Background(), addr, n.tlsConfig, &quic.Config{
		KeepAlivePeriod: 5 * time.Second,
		MaxIdleTimeout:  30 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("连接设备失败: %v", err)
	}

	n.mu.Lock()
	n.connections[deviceID] = conn
	n.mu.Unlock()

	return conn, nil
}

// sendOnConnection 在连接上发送消息
func (n *Network) sendOnConnection(conn *quic.Conn, msg *Message) error {
	data, err := msg.Encode()
	if err != nil {
		return err
	}

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		return err
	}
	defer stream.Close()

	// 写入消息长度
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))
	if _, err := stream.Write(lenBuf); err != nil {
		return err
	}

	// 写入消息内容
	_, err = stream.Write(data)
	return err
}

// generateSelfSignedCert 生成自签名证书
func (n *Network) generateSelfSignedCert() tls.Certificate {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal("生成密钥失败:", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour * 365 * 20),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		log.Fatal("创建证书失败:", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		log.Fatal("序列化密钥失败:", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		log.Fatal("加载证书失败:", err)
	}
	return tlsCert
}