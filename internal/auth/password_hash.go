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

// HashAlgorithm 标识存储哈希所用的算法。
type HashAlgorithm int

const (
	// HashBcrypt 是当前使用的算法。
	HashBcrypt HashAlgorithm = iota
	// HashLegacySHA256 是旧数据库中遗留的十六进制 SHA-256。
	HashLegacySHA256
)

// PasswordHash 包装存储的哈希，负责校验与升级。
type PasswordHash struct {
	hash string // bcrypt 哈希；兼容旧版十六进制 SHA-256
}

// NewPasswordHashFromStored 包装从数据库原样读出的哈希。
func NewPasswordHashFromStored(storedHash string) *PasswordHash {
	return &PasswordHash{hash: storedHash}
}

// Generate 为新密码生成 bcrypt 哈希。
func Generate(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// Verify 校验密码；命中旧版 SHA-256 时通过 onUpgrade 回调新 bcrypt 哈希供调用方持久化。
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

// Algorithm 报告存储哈希所用算法；未知格式回退为 HashBcrypt（我们只写出这一种格式）。
func (ph *PasswordHash) Algorithm() HashAlgorithm {
	if isBcryptHash(ph.hash) {
		return HashBcrypt
	}
	if isLegacySHA256Hash(ph.hash) {
		return HashLegacySHA256
	}
	return HashBcrypt
}

// IsDefaultPassword 判断存储哈希是否仍是 admin/admin。
func (ph *PasswordHash) IsDefaultPassword() bool {
	return ph.Verify(defaultPassword, nil)
}

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

// Upgrader 串行化旧哈希升级，避免并发登录时对同一行反复 bcrypt。
type Upgrader struct {
	mu      sync.Mutex
	pending map[string]time.Time // key -> upgrade start time
}

func NewUpgrader() *Upgrader {
	return &Upgrader{pending: make(map[string]time.Time)}
}

// TryUpgrade 经 saveFn 重哈希旧行；已升级或 5 分钟内重试过的 key 直接跳过。
func (u *Upgrader) TryUpgrade(key string, currentHash string, password string, saveFn func(newHash string) error) bool {
	if !isLegacySHA256Hash(currentHash) {
		return false
	}

	u.mu.Lock()
	now := time.Now()
	if startTime, exists := u.pending[key]; exists {
		if now.Sub(startTime) < 5*time.Minute {
			u.mu.Unlock()
			return false
		}
	}
	u.pending[key] = now
	u.mu.Unlock()

	// 二次检查：其他 goroutine 可能已并发升级该行。
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

// ErrInvalidPassword 表示密码不满足强度策略。
var ErrInvalidPassword = errors.New("password must be 8-72 characters")

// minPasswordLength 仅约束新设置的密码（既有短密码仍可登录）；4 位密码对在线爆破几乎无抵抗，锁定只是延缓。
const minPasswordLength = 8

// maxPasswordBytes 是 bcrypt 的有效输入上限（超出部分被静默截断），显式拒绝以免改密成功但登录失败。
const maxPasswordBytes = 72

// ValidatePassword 校验密码强度策略。
func ValidatePassword(password string) error {
	if len(password) < minPasswordLength {
		return ErrInvalidPassword
	}
	if len(password) > maxPasswordBytes {
		return ErrInvalidPassword
	}
	return nil
}
