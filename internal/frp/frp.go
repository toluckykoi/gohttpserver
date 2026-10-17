package frp

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/fatedier/frp/client"
	"github.com/fatedier/frp/pkg/config/source"
	"github.com/fatedier/frp/pkg/config/v1"
)

// FRPClient 管理 FRP 客户端的生命周期。
type FRPClient struct {
	mu         sync.Mutex
	enabled    bool
	cfg        *FRPConfig
	running    bool
	cancel     context.CancelFunc
	service    *client.Service
	stopCh     chan struct{}
	db         *sql.DB
	nameSuffix string // 进程级后缀，用于保证代理名唯一
}

// FRPConfig 保存 FRP 客户端配置。
type FRPConfig struct {
	ServerAddr           string // frps 主机（不含端口）
	ServerPort           int    // frps 端口
	AuthToken            string // frps 认证 token，落库前加密
	ProxyName            string // 注册到 frps 的代理名，不含进程级后缀
	ProxyType            string // frp 代理类型，如 "tcp"
	LocalIP              string // 代理转发到的本地地址
	LocalPort            int    // 代理转发到的本地端口
	RemotePort           int    // 在 frps 上开放的端口
	ProxyProtocolVersion string // v1 或 v2，为空表示禁用
}

// NewFRPClient 创建 FRP 客户端管理器。
func NewFRPClient(db *sql.DB) *FRPClient {
	return &FRPClient{
		enabled:    false,
		running:    false,
		db:         db,
		nameSuffix: randomProxySuffix(),
	}
}

// randomProxySuffix 返回 3 位随机字母数字串；frps 要求代理名全局唯一，
// 该后缀可避免共用同一 frps 的多个实例互相冲突。
func randomProxySuffix() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "xxx"
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b[:])
}

// registeredProxyName 返回发给 FRP 服务器的代理名（配置名 + 进程随机后缀）；
// 配置的 ProxyName 原样保留用于展示，只有注册值带后缀，避免重复保存时后缀叠加。
func (c *FRPClient) registeredProxyName() string {
	if c.cfg == nil {
		return ""
	}
	name := c.cfg.ProxyName
	if name == "" {
		name = "gohttpserver"
	}
	return name + "-" + c.nameSuffix
}

func (c *FRPClient) getOrCreateEncryptionKey() ([]byte, error) {
	keyFile := "frp-encryption.key"

	if data, err := os.ReadFile(keyFile); err == nil {
		if len(data) == 32 {
			return data, nil
		}
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate encryption key: %w", err)
	}

	if err := os.WriteFile(keyFile, key, 0600); err != nil {
		return nil, fmt.Errorf("save encryption key: %w", err)
	}

	return key, nil
}

func (c *FRPClient) encryptToken(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	key, err := c.getOrCreateEncryptionKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (c *FRPClient) decryptToken(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}

	// 过短或非 base64 的值按明文处理（旧格式）。
	if len(encoded) < 32 {
		return encoded, nil
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return encoded, nil
	}

	key, err := c.getOrCreateEncryptionKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := aesGCM.NonceSize()
	if len(ciphertext) < nonceSize {
		return encoded, nil
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}

// LoadFromDB 从数据库加载 FRP 配置及启用状态。
func (c *FRPClient) LoadFromDB() (*FRPConfig, bool, error) {
	if c.db == nil {
		return nil, false, nil
	}

	var cfg FRPConfig
	var enabled bool
	var encryptedToken string
	err := c.db.QueryRow(`
		SELECT enabled, server_addr, server_port, auth_token, proxy_name, proxy_type,
		       local_ip, local_port, remote_port, proxy_protocol_version
		FROM frp_config WHERE id = 1
	`).Scan(&enabled, &cfg.ServerAddr, &cfg.ServerPort, &encryptedToken,
		&cfg.ProxyName, &cfg.ProxyType, &cfg.LocalIP, &cfg.LocalPort, &cfg.RemotePort,
		&cfg.ProxyProtocolVersion)

	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	// 明文 token 就地迁移为加密格式。
	cfg.AuthToken, err = c.decryptToken(encryptedToken)
	if err != nil {
		return nil, false, fmt.Errorf("decrypt auth token: %w", err)
	}
	if encryptedToken != "" && encryptedToken == cfg.AuthToken {
		if err := c.SaveToDB(&cfg, enabled); err != nil {
			log.Printf("frp: failed to migrate auth token to encrypted format: %v", err)
		} else {
			log.Printf("frp: migrated auth token to encrypted format")
		}
	}

	return &cfg, enabled, nil
}

// SaveToDB 将 FRP 配置保存到数据库。
func (c *FRPClient) SaveToDB(cfg *FRPConfig, enabled bool) error {
	if c.db == nil {
		return nil
	}

	encryptedToken, err := c.encryptToken(cfg.AuthToken)
	if err != nil {
		return fmt.Errorf("encrypt auth token: %w", err)
	}

	_, err = c.db.Exec(`
		INSERT OR REPLACE INTO frp_config
		(id, enabled, server_addr, server_port, auth_token, proxy_name, proxy_type,
		 local_ip, local_port, remote_port, proxy_protocol_version, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, enabled, cfg.ServerAddr, cfg.ServerPort, encryptedToken, cfg.ProxyName,
		cfg.ProxyType, cfg.LocalIP, cfg.LocalPort, cfg.RemotePort, cfg.ProxyProtocolVersion, time.Now().Unix())

	return err
}

// UpdateConfig 更新 FRP 配置，必要时重启客户端。
func (c *FRPClient) UpdateConfig(cfg *FRPConfig) error {
	c.Stop()

	c.mu.Lock()
	c.cfg = cfg
	c.mu.Unlock()

	if err := c.SaveToDB(cfg, c.enabled); err != nil {
		log.Printf("FRP save config to DB error: %v", err)
	}

	if c.enabled && cfg != nil {
		return c.Start()
	}
	return nil
}

// SetEnabled 启用或禁用 FRP 客户端。
func (c *FRPClient) SetEnabled(enabled bool) error {
	c.mu.Lock()

	c.enabled = enabled

	if c.cfg != nil {
		if err := c.SaveToDB(c.cfg, enabled); err != nil {
			log.Printf("FRP save enabled state to DB error: %v", err)
		}
	}

	if enabled && c.cfg != nil {
		c.mu.Unlock()
		return c.Start()
	} else if !enabled {
		c.stopLocked()
	}
	c.mu.Unlock()
	return nil
}

// Start 启动 FRP 客户端。
func (c *FRPClient) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return nil
	}

	if c.cfg == nil {
		return fmt.Errorf("FRP config not set")
	}

	clientCfg := &v1.ClientCommonConfig{
		ServerAddr: c.cfg.ServerAddr,
		ServerPort: c.cfg.ServerPort,
		Auth: v1.AuthClientConfig{
			Method: "token",
			Token:  c.cfg.AuthToken,
		},
	}
	clientCfg.Complete()

	proxyCfg := &v1.TCPProxyConfig{
		ProxyBaseConfig: v1.ProxyBaseConfig{
			Name: c.registeredProxyName(),
			Type: c.cfg.ProxyType,
			Transport: v1.ProxyTransport{
				ProxyProtocolVersion: c.cfg.ProxyProtocolVersion,
			},
		},
		RemotePort: c.cfg.RemotePort,
	}
	proxyCfg.LocalIP = c.cfg.LocalIP
	proxyCfg.LocalPort = c.cfg.LocalPort

	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.stopCh = make(chan struct{})

	configSource := source.NewConfigSource()
	if err := configSource.ReplaceAll([]v1.ProxyConfigurer{proxyCfg}, nil); err != nil {
		cancel()
		close(c.stopCh)
		return fmt.Errorf("failed to set FRP proxies: %w", err)
	}

	aggregator := source.NewAggregator(configSource)

	svc, err := client.NewService(client.ServiceOptions{
		Common:                 clientCfg,
		ConfigSourceAggregator: aggregator,
	})
	if err != nil {
		cancel()
		close(c.stopCh)
		return fmt.Errorf("failed to create FRP service: %w", err)
	}
	c.service = svc

	// stopCh 先存局部变量再进 goroutine，避免与 Stop() 竞争（直接关 c.stopCh 不安全）。
	stopCh := c.stopCh
	go func() {
		defer func() {
			c.mu.Lock()
			c.running = false
			c.mu.Unlock()
			if stopCh != nil {
				close(stopCh)
			}
			if r := recover(); r != nil {
				log.Printf("FRP client panic: %v", r)
			}
		}()

		log.Printf("FRP client starting: %s:%d -> %s:%d (proxy: %s)",
			c.cfg.LocalIP, c.cfg.LocalPort,
			c.cfg.ServerAddr, c.cfg.RemotePort, c.registeredProxyName())

		if err := svc.Run(ctx); err != nil {
			log.Printf("FRP client error: %v", err)
		}
	}()

	c.running = true
	log.Println("FRP client started")
	return nil
}

// stopLocked 停止 FRP 客户端；调用方必须持有锁。
func (c *FRPClient) stopLocked() {
	if !c.running {
		return
	}

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	if c.service != nil {
		c.service.Close()
		c.service = nil
	}

	c.running = false
	log.Printf("FRP client stop signal sent")
}

// Stop 停止 FRP 客户端并等待其退出。
func (c *FRPClient) Stop() {
	c.mu.Lock()

	if !c.running {
		c.mu.Unlock()
		return
	}

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	if c.service != nil {
		c.service.Close()
		c.service = nil
	}

	c.running = false

	// 释放锁前先取出 stopCh。
	stopCh := c.stopCh
	c.stopCh = nil
	c.mu.Unlock()

	// 在锁外等待，避免死锁。
	if stopCh != nil {
		select {
		case <-stopCh:
			log.Printf("FRP client stopped")
		case <-time.After(5 * time.Second):
			log.Printf("FRP client stop timeout")
		}
	}
}

// Status 返回 FRP 客户端当前状态。
func (c *FRPClient) Status() map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	status := map[string]interface{}{
		"enabled": c.enabled,
		"running": c.running,
	}

	if c.cfg != nil {
		// 绝不通过 API 暴露 token 值；authTokenSet 让界面无需持有密钥即可显示"已配置"。
		status["config"] = map[string]interface{}{
			"serverAddr":           c.cfg.ServerAddr,
			"serverPort":           c.cfg.ServerPort,
			"authToken":            "",
			"authTokenSet":         c.cfg.AuthToken != "",
			"proxyName":            c.cfg.ProxyName,
			"registeredProxyName":  c.registeredProxyName(),
			"proxyType":            c.cfg.ProxyType,
			"localIP":              c.cfg.LocalIP,
			"localPort":            c.cfg.LocalPort,
			"remotePort":           c.cfg.RemotePort,
			"proxyProtocolVersion": c.cfg.ProxyProtocolVersion,
		}
	}

	return status
}
