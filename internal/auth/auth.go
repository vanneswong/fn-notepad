// Package auth implements the small account system behind the notes app: a
// single JSON file of users with PBKDF2-hashed passwords, plus in-memory
// login sessions and a brute-force limiter.
//
// It deliberately depends on nothing but the standard library, so the app
// keeps shipping as one self-contained binary.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// CookieName is the name of the session cookie the HTTP layer sets.
const CookieName = "notepad_session"

// Errors are user-facing and therefore written in Chinese, like the rest of
// the app.
var (
	ErrInvalidUsername = errors.New("用户名不合法：1-32 个字符，不能包含空格、斜杠或控制字符")
	ErrUsernameTaken   = errors.New("该用户名已被使用")
	ErrWeakPassword    = errors.New("密码至少需要 6 位")
	ErrPasswordTooLong = errors.New("密码过长")
	ErrBadCredentials  = errors.New("用户名或密码不正确")
	ErrWrongPassword   = errors.New("当前密码不正确")
	ErrNoUsers         = errors.New("尚未创建账号")
	ErrAlreadySetup    = errors.New("账号已存在，无法重复创建")
	ErrSamePassword    = errors.New("新密码不能与当前密码相同")
)

const (
	hashScheme     = "pbkdf2-sha256"
	hashIterations = 120000
	hashKeyLen     = 32
	saltLen        = 16

	minPasswordRunes = 6
	maxPasswordBytes = 256
	maxUsernameRunes = 32

	fileMode = 0o600
)

// User is the stored (and, without PasswordHash, published) view of an account.
type User struct {
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash,omitempty"`
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
}

// userFile is the on-disk shape of the user database.
type userFile struct {
	Version int    `json:"version"`
	Users   []User `json:"users"`
}

const userFileVersion = 1

// Store owns a JSON file that holds every account.
type Store struct {
	path string

	mu  sync.RWMutex
	doc userFile
}

// New opens (or prepares) the user database at path. A missing file is not an
// error: the app then starts in "first run" mode and asks for an account.
func New(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	s := &Store{path: abs, doc: userFile{Version: userFileVersion}}

	data, err := os.ReadFile(abs)
	switch {
	case err == nil:
		var doc userFile
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("解析用户文件 %s 失败：%w", abs, err)
		}
		if doc.Users == nil {
			doc.Users = []User{}
		}
		doc.Version = userFileVersion
		s.doc = doc
	case os.IsNotExist(err):
		// First run: nothing to load.
	default:
		return nil, err
	}
	return s, nil
}

// Path reports the absolute path of the user database.
func (s *Store) Path() string { return s.path }

// Configured reports whether at least one account exists.
func (s *Store) Configured() bool { return s.Count() > 0 }

// Count returns how many accounts exist.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.doc.Users)
}

// Usernames lists every account name, oldest first.
func (s *Store) Usernames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.doc.Users))
	for _, u := range s.doc.Users {
		names = append(names, u.Username)
	}
	return names
}

// Create adds a new account with a hashed password.
func (s *Store) Create(username, password string) (User, error) {
	name, err := NormalizeUsername(username)
	if err != nil {
		return User{}, err
	}
	if err := checkPassword(password); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := strings.ToLower(name)
	for _, u := range s.doc.Users {
		if strings.ToLower(u.Username) == key {
			return User{}, ErrUsernameTaken
		}
	}

	now := time.Now().UnixMilli()
	user := User{Username: name, PasswordHash: hash, CreatedAt: now, UpdatedAt: now}
	s.doc.Users = append(s.doc.Users, user)
	if err := s.saveLocked(); err != nil {
		s.doc.Users = s.doc.Users[:len(s.doc.Users)-1]
		return User{}, err
	}
	return user, nil
}

// Authenticate verifies a username/password pair.
func (s *Store) Authenticate(username, password string) (User, error) {
	name := strings.TrimSpace(username)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.doc.Users) == 0 {
		return User{}, ErrNoUsers
	}
	key := strings.ToLower(name)
	for _, u := range s.doc.Users {
		if strings.ToLower(u.Username) != key {
			continue
		}
		if VerifyPassword(u.PasswordHash, password) {
			return u, nil
		}
		break
	}
	return User{}, ErrBadCredentials
}

// ChangePassword replaces the password of an existing account after checking
// the current one.
func (s *Store) ChangePassword(username, current, next string) error {
	if err := checkPassword(next); err != nil {
		return err
	}
	if next == current {
		return ErrSamePassword
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := strings.ToLower(strings.TrimSpace(username))
	for i, u := range s.doc.Users {
		if strings.ToLower(u.Username) != key {
			continue
		}
		if !VerifyPassword(u.PasswordHash, current) {
			return ErrWrongPassword
		}
		hash, err := HashPassword(next)
		if err != nil {
			return err
		}
		previous := u.PasswordHash
		s.doc.Users[i].PasswordHash = hash
		s.doc.Users[i].UpdatedAt = time.Now().UnixMilli()
		if err := s.saveLocked(); err != nil {
			s.doc.Users[i].PasswordHash = previous
			return err
		}
		return nil
	}
	return ErrBadCredentials
}

// saveLocked writes the database atomically. Callers must hold s.mu.
func (s *Store) saveLocked() error {
	s.doc.Version = userFileVersion
	if s.doc.Users == nil {
		s.doc.Users = []User{}
	}
	data, err := json.MarshalIndent(s.doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".users-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, fileMode); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}
	tmpName = ""
	return nil
}

// NormalizeUsername validates and trims an account name.
func NormalizeUsername(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > maxUsernameRunes {
		return "", ErrInvalidUsername
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", ErrInvalidUsername
		}
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", ErrInvalidUsername
	}
	return name, nil
}

func checkPassword(password string) error {
	if utf8.RuneCountInString(password) < minPasswordRunes {
		return ErrWeakPassword
	}
	if len(password) > maxPasswordBytes {
		return ErrPasswordTooLong
	}
	return nil
}

// HashPassword returns "pbkdf2-sha256$<iterations>$<salt>$<hash>", both halves
// base64 (no padding) so the value stays a plain JSON string.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := pbkdf2SHA256([]byte(password), salt, hashIterations, hashKeyLen)
	return fmt.Sprintf("%s$%d$%s$%s", hashScheme, hashIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum)), nil
}

// VerifyPassword reports whether password matches an encoded hash. A malformed
// hash verifies as false instead of panicking.
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != hashScheme {
		return false
	}
	var iterations int
	if _, err := fmt.Sscanf(parts[1], "%d", &iterations); err != nil || iterations <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, iterations, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// pbkdf2SHA256 is a self-contained PBKDF2-HMAC-SHA256 (RFC 2898) so the app
// needs no third-party module.
func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	blocks := (keyLen + hashLen - 1) / hashLen

	var buf [4]byte
	dk := make([]byte, 0, blocks*hashLen)
	u := make([]byte, hashLen)
	for block := 1; block <= blocks; block++ {
		prf.Reset()
		prf.Write(salt)
		buf[0] = byte(block >> 24)
		buf[1] = byte(block >> 16)
		buf[2] = byte(block >> 8)
		buf[3] = byte(block)
		prf.Write(buf[:4])
		dk = prf.Sum(dk)
		t := dk[len(dk)-hashLen:]
		copy(u, t)
		for n := 2; n <= iterations; n++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for i := range u {
				t[i] ^= u[i]
			}
		}
	}
	return dk[:keyLen]
}
