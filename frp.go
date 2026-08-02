package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
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
	ServerAddr string
	ServerPort int
	AuthToken  string
	ProxyName  string
	ProxyType  string
	LocalIP    string
	LocalPort  int
	RemotePort int
}

// NewFRPClient creates a new FRP client manager
func NewFRPClient(db *sql.DB) *FRPClient {
	return &FRPClient{
		enabled: false,
		running: false,
		db:      db,
	}
}

// LoadFromDB loads FRP configuration from database
func (c *FRPClient) LoadFromDB() (*FRPConfig, bool, error) {
	if c.db == nil {
		return nil, false, nil
	}

	var cfg FRPConfig
	var enabled bool
	err := c.db.QueryRow(`
		SELECT enabled, server_addr, server_port, auth_token, proxy_name, proxy_type, 
		       local_ip, local_port, remote_port 
		FROM frp_config WHERE id = 1
	`).Scan(&enabled, &cfg.ServerAddr, &cfg.ServerPort, &cfg.AuthToken, 
		&cfg.ProxyName, &cfg.ProxyType, &cfg.LocalIP, &cfg.LocalPort, &cfg.RemotePort)

	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	return &cfg, enabled, nil
}

// SaveToDB saves FRP configuration to database
func (c *FRPClient) SaveToDB(cfg *FRPConfig, enabled bool) error {
	if c.db == nil {
		return nil
	}

	_, err := c.db.Exec(`
		INSERT OR REPLACE INTO frp_config 
		(id, enabled, server_addr, server_port, auth_token, proxy_name, proxy_type, 
		 local_ip, local_port, remote_port, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, enabled, cfg.ServerAddr, cfg.ServerPort, cfg.AuthToken, cfg.ProxyName, 
		cfg.ProxyType, cfg.LocalIP, cfg.LocalPort, cfg.RemotePort, time.Now().Unix())

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
	log.Printf("FRP client started")
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
		status["config"] = map[string]interface{}{
			"serverAddr": c.cfg.ServerAddr,
			"serverPort": c.cfg.ServerPort,
			"authToken":  c.cfg.AuthToken,
			"proxyName":  c.cfg.ProxyName,
			"proxyType":  c.cfg.ProxyType,
			"localIP":    c.cfg.LocalIP,
			"localPort":  c.cfg.LocalPort,
			"remotePort": c.cfg.RemotePort,
		}
	}

	return status
}
