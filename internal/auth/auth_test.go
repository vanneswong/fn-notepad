package auth

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// RFC-style test vectors for PBKDF2-HMAC-SHA256 (password="password",
// salt="salt", dkLen=32). Guards the hand-rolled KDF.
func TestPBKDF2Vectors(t *testing.T) {
	cases := []struct {
		iterations int
		want       string
	}{
		{1, "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"},
		{2, "ae4d0c95af6b46d32d0adff928f06dd02a303f8ef3c251dfd6e2d85a95474c43"},
		{4096, "c5e478d59288c841aa530db6845c4c8d962893a001ce4e11a4963873aa98134a"},
	}
	for _, c := range cases {
		got := hex.EncodeToString(pbkdf2SHA256([]byte("password"), []byte("salt"), c.iterations, 32))
		if got != c.want {
			t.Errorf("pbkdf2(iterations=%d) = %s, want %s", c.iterations, got, c.want)
		}
	}
}

func TestHashAndVerify(t *testing.T) {
	hash, err := HashPassword("hunter2000")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, "hunter2000") {
		t.Fatalf("hash leaks the password: %s", hash)
	}
	if !strings.HasPrefix(hash, hashScheme+"$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}
	if !VerifyPassword(hash, "hunter2000") {
		t.Fatal("correct password should verify")
	}
	if VerifyPassword(hash, "hunter2001") {
		t.Fatal("wrong password must not verify")
	}

	// Two hashes of the same password differ (random salt) but both verify.
	other, err := HashPassword("hunter2000")
	if err != nil {
		t.Fatal(err)
	}
	if other == hash {
		t.Fatal("salt is not random")
	}
	if !VerifyPassword(other, "hunter2000") {
		t.Fatal("second hash should verify")
	}

	// Malformed input never panics and never verifies.
	for _, bad := range []string{"", "junk", "pbkdf2-sha256$0$a$b", "pbkdf2-sha256$abc$a$b", "scrypt$1$a$b"} {
		if VerifyPassword(bad, "hunter2000") {
			t.Errorf("malformed hash %q verified", bad)
		}
	}
}

func TestCreateAuthenticateAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Configured() {
		t.Fatal("fresh store should not be configured")
	}
	if _, err := s.Authenticate("admin", "secret123"); !errors.Is(err, ErrNoUsers) {
		t.Fatalf("authenticate without users = %v, want ErrNoUsers", err)
	}

	user, err := s.Create("  老王 ", "secret123")
	if err != nil {
		t.Fatal(err)
	}
	if user.Username != "老王" {
		t.Fatalf("username = %q, want 老王", user.Username)
	}
	if !s.Configured() || s.Count() != 1 {
		t.Fatalf("configured=%v count=%d", s.Configured(), s.Count())
	}

	// Duplicates are refused regardless of case.
	if _, err := s.Create("老王", "secret123"); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("duplicate = %v, want ErrUsernameTaken", err)
	}
	if _, err := s.Create("  老王  ", "secret123"); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("trimmed duplicate = %v, want ErrUsernameTaken", err)
	}

	// Name and password rules.
	for _, name := range []string{"", "  ", "a/b", `a\b`, "..x", strings.Repeat("x", maxUsernameRunes+1), "bad\x00name"} {
		if _, err := s.Create(name, "secret123"); !errors.Is(err, ErrInvalidUsername) {
			t.Errorf("Create(%q) = %v, want ErrInvalidUsername", name, err)
		}
	}
	for _, pass := range []string{"", "12345", "     "} {
		if _, err := s.Create("someone", pass); !errors.Is(err, ErrWeakPassword) {
			t.Errorf("Create password %q = %v, want ErrWeakPassword", pass, err)
		}
	}

	if _, err := s.Authenticate("老王", "secret123"); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if _, err := s.Authenticate("老王", "wrong"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("wrong password = %v, want ErrBadCredentials", err)
	}
	if _, err := s.Authenticate("nobody", "secret123"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("unknown user = %v, want ErrBadCredentials", err)
	}

	// The account survives a restart of the service.
	reopened, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Authenticate("老王", "secret123"); err != nil {
		t.Fatalf("authenticate after reopen: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != fileMode {
		t.Fatalf("user file mode = %o, want %o", info.Mode().Perm(), fileMode)
	}
	// No temporary files are left behind.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("unexpected files in data dir: %d", len(entries))
	}
}

func TestChangePassword(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("admin", "secret123"); err != nil {
		t.Fatal(err)
	}

	if err := s.ChangePassword("admin", "nope", "newsecret1"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong current = %v, want ErrWrongPassword", err)
	}
	if err := s.ChangePassword("admin", "secret123", "abc"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("weak new password = %v, want ErrWeakPassword", err)
	}
	if err := s.ChangePassword("admin", "secret123", "secret123"); !errors.Is(err, ErrSamePassword) {
		t.Fatalf("same password = %v, want ErrSamePassword", err)
	}
	if err := s.ChangePassword("admin", "secret123", "newsecret1"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if _, err := s.Authenticate("admin", "secret123"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("old password still works: %v", err)
	}
	if _, err := s.Authenticate("admin", "newsecret1"); err != nil {
		t.Fatalf("new password rejected: %v", err)
	}
}

func TestSessions(t *testing.T) {
	s := NewSessions()
	now := time.Now()
	s.now = func() time.Time { return now }

	token, expires := s.Start("admin", false)
	if token == "" {
		t.Fatal("empty token")
	}
	if got := expires.Sub(now); got != DefaultTTL {
		t.Fatalf("default lifetime = %s, want %s", got, DefaultTTL)
	}
	if user, ok := s.Lookup(token); !ok || user != "admin" {
		t.Fatalf("lookup = %q, %v", user, ok)
	}

	remembered, expires := s.Start("admin", true)
	if got := expires.Sub(now); got != RememberTTL {
		t.Fatalf("remember lifetime = %s, want %s", got, RememberTTL)
	}
	if user, ok := s.Lookup(remembered); !ok || user != "admin" {
		t.Fatalf("lookup remembered = %q, %v", user, ok)
	}

	// Expiry.
	now = now.Add(DefaultTTL + time.Minute)
	if _, ok := s.Lookup(token); ok {
		t.Fatal("expired session still valid")
	}
	if _, ok := s.Lookup(remembered); !ok {
		t.Fatal("remembered session should outlive the plain one")
	}

	// Password change on another device drops the other sessions only.
	if dropped := s.StopUser("admin", remembered); dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}
	third, _ := s.Start("admin", false)
	if dropped := s.StopUser("admin", remembered); dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	if _, ok := s.Lookup(third); ok {
		t.Fatal("StopUser left another session alive")
	}
	if _, ok := s.Lookup(remembered); !ok {
		t.Fatal("StopUser dropped the kept session")
	}

	s.Stop(remembered)
	if _, ok := s.Lookup(remembered); ok {
		t.Fatal("Stop did not drop the session")
	}
	if user, ok := s.Lookup(""); ok || user != "" {
		t.Fatal("empty token must not authenticate")
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(3, time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }

	if wait := l.RetryAfter("k"); wait != 0 {
		t.Fatalf("fresh key wait = %s", wait)
	}
	for i := 0; i < 3; i++ {
		l.Fail("k")
	}
	if wait := l.RetryAfter("k"); wait <= 0 {
		t.Fatal("expected a cool-down after max failures")
	}
	if wait := l.RetryAfter("other"); wait != 0 {
		t.Fatal("cool-down must be per key")
	}

	now = now.Add(time.Minute + time.Second)
	if wait := l.RetryAfter("k"); wait != 0 {
		t.Fatalf("wait after window = %s, want 0", wait)
	}

	l.Fail("k")
	l.Reset("k")
	if wait := l.RetryAfter("k"); wait != 0 {
		t.Fatalf("wait after reset = %s, want 0", wait)
	}
}
