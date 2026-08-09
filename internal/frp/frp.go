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

// FRPClient manages the FRP client lifecycle
type FRPClient struct {
	mu      sync.Mutex
	enabled bool
	cfg     *FRPConfig
	running bool
	cancel  context.CancelFunc
	service *client.Service
	stopCh  chan struct{}
	db      *sql.DB
}

// FRPConfig holds the FRP client configuration
type FRPConfig struct {
	ServerAddr          string
	ServerPort          int
	AuthToken           string
	ProxyName           string
	ProxyType           string
	LocalIP             string
	LocalPort           int
	RemotePort          int
	ProxyProtocolVersion string // v1 or v2, empty means disabled
}

// NewFRPClient creates a new FRP client manager
func NewFRPClient(db *sql.DB) *FRPClient {
	return &FRPClient{
		enabled: false,
		running: false,
		db:      db,
	}
}

// getOrCreateEncryptionKey loads or creates the FRP encryption key
func (c *FRPClient) getOrCreateEncryptionKey() ([]byte, error) {
	keyFile := "frp-encryption.key"
	
	// Try to load existing key
	if data, err := os.ReadFile(keyFile); err == nil {
		if len(data) == 32 {
			return data, nil
		}
	}
	
	// Generate new 256-bit key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate encryption key: %w", err)
	}
	
	// Save to file with restricted permissions
	if err := os.WriteFile(keyFile, key, 0600); err != nil {
		return nil, fmt.Errorf("save encryption key: %w", err)
	}
	
	return key, nil
}

// encryptToken encrypts the auth token using AES-GCM
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

// decryptToken decrypts the auth token using AES-GCM
func (c *FRPClient) decryptToken(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	
	// If it doesn't look like base64-encoded ciphertext, treat as plaintext (legacy)
	if len(encoded) < 32 {
		return encoded, nil
	}
	
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		// Not valid base64, treat as plaintext (legacy)
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
		return encoded, nil // Too short, treat as plaintext
	}
	
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	
	return string(plaintext), nil
}

// LoadFromDB loads FRP configuration from database
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

	// Decrypt auth token
	cfg.AuthToken, err = c.decryptToken(encryptedToken)
	if err != nil {
		return nil, false, fmt.Errorf("decrypt auth token: %w", err)
	}

	// Migrate plaintext to encrypted if needed
	if encryptedToken != "" && encryptedToken == cfg.AuthToken {
		if err := c.SaveToDB(&cfg, enabled); err != nil {
			log.Printf("frp: failed to migrate auth token to encrypted format: %v", err)
		} else {
			log.Printf("frp: migrated auth token to encrypted format")
		}
	}

	return &cfg, enabled, nil
}

// SaveToDB saves FRP configuration to database
func (c *FRPClient) SaveToDB(cfg *FRPConfig, enabled bool) error {
	if c.db == nil {
		return nil
	}

	// Encrypt auth token before saving
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

// UpdateConfig updates the FRP configuration and restarts if needed
func (c *FRPClient) UpdateConfig(cfg *FRPConfig) error {
	// First stop any existing client and wait for it to finish
	c.Stop()

	c.mu.Lock()
	c.cfg = cfg
	c.mu.Unlock()

	// Save to database
	if err := c.SaveToDB(cfg, c.enabled); err != nil {
		log.Printf("FRP save config to DB error: %v", err)
	}

	// Start if enabled
	if c.enabled && cfg != nil {
		return c.Start()
	}
	return nil
}

// SetEnabled enables or disables the FRP client
func (c *FRPClient) SetEnabled(enabled bool) error {
	c.mu.Lock()

	c.enabled = enabled

	// Save to database
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

// Start starts the FRP client
func (c *FRPClient) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return nil
	}

	if c.cfg == nil {
		return fmt.Errorf("FRP config not set")
	}

	// Build FRP client common configuration
	clientCfg := &v1.ClientCommonConfig{
		ServerAddr: c.cfg.ServerAddr,
		ServerPort: c.cfg.ServerPort,
		Auth: v1.AuthClientConfig{
			Method: "token",
			Token:  c.cfg.AuthToken,
		},
	}
	clientCfg.Complete()

	// Build proxy configuration
	proxyCfg := &v1.TCPProxyConfig{
		ProxyBaseConfig: v1.ProxyBaseConfig{
			Name: c.cfg.ProxyName,
			Type: c.cfg.ProxyType,
			Transport: v1.ProxyTransport{
				ProxyProtocolVersion: c.cfg.ProxyProtocolVersion,
			},
		},
		RemotePort: c.cfg.RemotePort,
	}
	proxyCfg.LocalIP = c.cfg.LocalIP
	proxyCfg.LocalPort = c.cfg.LocalPort

	// Create context with cancel
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.stopCh = make(chan struct{})

	// Create config source and set proxies
	configSource := source.NewConfigSource()
	if err := configSource.ReplaceAll([]v1.ProxyConfigurer{proxyCfg}, nil); err != nil {
		cancel()
		close(c.stopCh)
		return fmt.Errorf("failed to set FRP proxies: %w", err)
	}

	// Create aggregator
	aggregator := source.NewAggregator(configSource)

	// Create FRP service
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

	// Start FRP client in goroutine
	// Save stopCh to local variable to avoid race condition with Stop()
	stopCh := c.stopCh
	go func() {
		defer func() {
			c.mu.Lock()
			c.running = false
			c.mu.Unlock()
			// Close the local stopCh, not c.stopCh (which might be nil after Stop())
			if stopCh != nil {
				close(stopCh)
			}
			if r := recover(); r != nil {
				log.Printf("FRP client panic: %v", r)
			}
		}()

		log.Printf("FRP client starting: %s:%d -> %s:%d",
			c.cfg.LocalIP, c.cfg.LocalPort,
			c.cfg.ServerAddr, c.cfg.RemotePort)

		// Run FRP service
		if err := svc.Run(ctx); err != nil {
			log.Printf("FRP client error: %v", err)
		}
	}()

	c.running = true
	log.Println("FRP client started")
	return nil
}

// stopLocked stops the FRP client (must be called with lock held)
// This version does NOT wait for the goroutine to avoid deadlock
func (c *FRPClient) stopLocked() {
	if !c.running {
		return
	}

	// Cancel context to signal goroutine to stop
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	// Close service
	if c.service != nil {
		c.service.Close()
		c.service = nil
	}

	c.running = false
	log.Printf("FRP client stop signal sent")
}

// Stop stops the FRP client and waits for it to finish
func (c *FRPClient) Stop() {
	c.mu.Lock()

	if !c.running {
		c.mu.Unlock()
		return
	}

	// Cancel context to signal goroutine to stop
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	// Close service
	if c.service != nil {
		c.service.Close()
		c.service = nil
	}

	c.running = false

	// Get stopCh before releasing lock
	stopCh := c.stopCh
	c.stopCh = nil
	c.mu.Unlock()

	// Wait for goroutine to finish (outside lock to avoid deadlock)
	if stopCh != nil {
		select {
		case <-stopCh:
			log.Printf("FRP client stopped")
		case <-time.After(5 * time.Second):
			log.Printf("FRP client stop timeout")
		}
	}
}

// Status returns the current FRP client status
func (c *FRPClient) Status() map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	status := map[string]interface{}{
		"enabled": c.enabled,
		"running": c.running,
	}

	if c.cfg != nil {
		// Mask auth token to prevent credential leakage via API.
		// Only show whether a token is configured, never its value.
		tokenMasked := ""
		if c.cfg.AuthToken != "" {
			tokenMasked = "********"
		}
		status["config"] = map[string]interface{}{
			"serverAddr":           c.cfg.ServerAddr,
			"serverPort":           c.cfg.ServerPort,
			"authToken":            tokenMasked,
			"proxyName":            c.cfg.ProxyName,
			"proxyType":            c.cfg.ProxyType,
			"localIP":              c.cfg.LocalIP,
			"localPort":            c.cfg.LocalPort,
			"remotePort":           c.cfg.RemotePort,
			"proxyProtocolVersion": c.cfg.ProxyProtocolVersion,
		}
	}

	return status
}
