package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// HashAlgorithm 标识密码哈希算法
type HashAlgorithm int

const (
	// HashBcrypt 当前推荐算法
	HashBcrypt HashAlgorithm = iota
	// HashLegacySHA256 遗留 SHA-256 十六进制
	HashLegacySHA256
)

// PasswordHash 统一密码哈希接口，封装算法识别、验证、升级
type PasswordHash struct {
	hash string // 存储的哈希值（bcrypt 或十六进制 SHA-256）
}

// NewPasswordHashFromStored 从数据库读取的哈希创建对象
func NewPasswordHashFromStored(storedHash string) *PasswordHash {
	return &PasswordHash{hash: storedHash}
}

// Generate 生成新密码的 bcrypt 哈希
func Generate(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// Verify 验证密码，支持 bcrypt 和遗留 SHA-256；验证成功且为遗留算法时自动异步升级
func (ph *PasswordHash) Verify(password string, onUpgrade func(newHash string)) bool {
	if ph.hash == "" {
		return false
	}

	if isBcryptHash(ph.hash) {
		return bcrypt.CompareHashAndPassword([]byte(ph.hash), []byte(password)) == nil
	}

	if isLegacySHA256Hash(ph.hash) {
		expected := sha256Hex(password)
		if subtle.ConstantTimeCompare([]byte(expected), []byte(ph.hash)) == 1 {
			// 遗留算法匹配，触发升级回调
			if onUpgrade != nil {
				newHash, err := Generate(password)
				if err == nil {
					onUpgrade(newHash)
				}
			}
			return true
		}
		return false
	}

	return false
}

// Algorithm 返回当前哈希算法类型
func (ph *PasswordHash) Algorithm() HashAlgorithm {
	if isBcryptHash(ph.hash) {
		return HashBcrypt
	}
	if isLegacySHA256Hash(ph.hash) {
		return HashLegacySHA256
	}
	return HashBcrypt // 默认按 bcrypt 处理
}

// IsDefaultPassword 检查是否为默认密码 admin/admin
func (ph *PasswordHash) IsDefaultPassword() bool {
	return ph.Verify(defaultPassword, nil)
}

// ─── 内部辅助函数 ───

const defaultPassword = "admin"

func isBcryptHash(hash string) bool {
	return strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$")
}

func isLegacySHA256Hash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ─── 线程安全的升级器 ───

// Upgrader 管理遗留哈希到 bcrypt 的异步升级，避免并发重复升级
type Upgrader struct {
	mu      sync.Mutex
	pending map[string]time.Time // key -> 升级开始时间
}

func NewUpgrader() *Upgrader {
	return &Upgrader{pending: make(map[string]time.Time)}
}

// TryUpgrade 尝试升级；若已在升级中或 5 分钟内已升级过则跳过
func (u *Upgrader) TryUpgrade(key string, currentHash string, password string, saveFn func(newHash string) error) bool {
	if !isLegacySHA256Hash(currentHash) {
		return false
	}

	u.mu.Lock()
	now := time.Now()
	if startTime, exists := u.pending[key]; exists {
		// 5 分钟内已尝试过，跳过
		if now.Sub(startTime) < 5*time.Minute {
			u.mu.Unlock()
			return false
		}
	}
	u.pending[key] = now
	u.mu.Unlock()

	// 双重检查：升级期间可能已被其他 goroutine 更新
	if !isLegacySHA256Hash(currentHash) {
		u.mu.Lock()
		delete(u.pending, key)
		u.mu.Unlock()
		return false
	}

	newHash, err := Generate(password)
	if err != nil {
		log.Printf("password: generate upgrade hash failed: %v", err)
		u.mu.Lock()
		delete(u.pending, key)
		u.mu.Unlock()
		return false
	}

	if err := saveFn(newHash); err != nil {
		log.Printf("password: save upgraded hash failed: %v", err)
		u.mu.Lock()
		delete(u.pending, key)
		u.mu.Unlock()
		return false
	}

	log.Printf("password: successfully upgraded legacy hash to bcrypt for %s", key)
	u.mu.Lock()
	delete(u.pending, key)
	u.mu.Unlock()
	return true
}

// ErrInvalidPassword 密码不符合策略
var ErrInvalidPassword = errors.New("password must be at least 4 characters")

// ValidatePassword 校验密码强度（最小长度 4）
func ValidatePassword(password string) error {
	if len(password) < 4 {
		return ErrInvalidPassword
	}
	return nil
}