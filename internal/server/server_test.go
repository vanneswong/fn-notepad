package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vanneswong/fnos-notepad/internal/auth"
	"github.com/vanneswong/fnos-notepad/internal/oplog"
	"github.com/vanneswong/fnos-notepad/internal/store"
)

const (
	testUser = "admin"
	testPass = "secret123"
)

func newHandler(t *testing.T, dir, prefix string) (http.Handler, *auth.Store) {
	t.Helper()
	// 与生产布局一致：笔记在 <dir>/notes，用户文件是它的同级 users.json。
	st, err := store.New(filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := auth.New(filepath.Join(dir, "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	logs, err := oplog.New(filepath.Join(dir, "oplog.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{
		Name:    "记事本",
		Version: "test",
		Store:   st,
		Auth:    accounts,
		Logs:    logs,
		Prefix:  prefix,
	}), accounts
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	h, _ := newHandler(t, t.TempDir(), "")
	return h
}

// newAuthedHandler returns a handler with a first-run account already created,
// plus the session cookie of that account.
func newAuthedHandler(t *testing.T) (http.Handler, *http.Cookie) {
	t.Helper()
	h, _ := newHandler(t, t.TempDir(), "")
	rec := do(t, h, http.MethodPost, "/api/setup",
		`{"username":"`+testUser+`","password":"`+testPass+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup status = %d body=%s", rec.Code, rec.Body.String())
	}
	return h, sessionCookie(t, rec)
}

func doWith(t *testing.T, h http.Handler, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return doWith(t, h, nil, method, path, body)
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	t.Fatalf("no %s cookie in response (headers: %v)", auth.CookieName, rec.Result().Header)
	return nil
}

func TestAPIRoundTrip(t *testing.T) {
	h, cookie := newAuthedHandler(t)

	if rec := doWith(t, h, cookie, http.MethodGet, "/api/info", ""); rec.Code != 200 {
		t.Fatalf("info status = %d", rec.Code)
	}

	rec := doWith(t, h, cookie, http.MethodPost, "/api/notes", `{"name":"日记"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Note store.Note `json:"note"`
	}
	mustJSON(t, rec.Body.Bytes(), &created)
	if created.Note.Name != "日记.txt" {
		t.Fatalf("created = %q", created.Note.Name)
	}

	rec = doWith(t, h, cookie, http.MethodPut, "/api/notes/日记.txt", `{"content":"第一条\n第二条"}`)
	if rec.Code != 200 {
		t.Fatalf("write status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = doWith(t, h, cookie, http.MethodGet, "/api/notes/日记.txt", "")
	var got struct {
		Note    store.Note `json:"note"`
		Content string     `json:"content"`
	}
	mustJSON(t, rec.Body.Bytes(), &got)
	if got.Content != "第一条\n第二条" {
		t.Fatalf("content = %q", got.Content)
	}

	rec = doWith(t, h, cookie, http.MethodGet, "/api/notes", "")
	var list struct {
		Notes []store.Note `json:"notes"`
	}
	mustJSON(t, rec.Body.Bytes(), &list)
	if len(list.Notes) != 1 {
		t.Fatalf("notes = %d", len(list.Notes))
	}

	rec = doWith(t, h, cookie, http.MethodPost, "/api/notes/日记.txt/rename", `{"name":"随笔"}`)
	if rec.Code != 200 {
		t.Fatalf("rename status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = doWith(t, h, cookie, http.MethodDelete, "/api/notes/随笔.txt", "")
	if rec.Code != 200 {
		t.Fatalf("delete status = %d", rec.Code)
	}

	if rec := doWith(t, h, cookie, http.MethodGet, "/api/notes/随笔.txt", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("missing note status = %d, want 404", rec.Code)
	}
}

func TestAPIRejectsBadInput(t *testing.T) {
	h, cookie := newAuthedHandler(t)

	if rec := doWith(t, h, cookie, http.MethodPut, "/api/notes/bad", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing content status = %d, want 400", rec.Code)
	}
	if rec := doWith(t, h, cookie, http.MethodPost, "/api/notes", `{"name":"../evil"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("traversal create status = %d, want 400", rec.Code)
	}
	if rec := doWith(t, h, cookie, http.MethodPost, "/api/notes", `not json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d, want 400", rec.Code)
	}
}

// Every note endpoint must refuse anonymous callers, and accept the logged-in
// one (so the 401s are not just broken routing).
func TestNotesRequireLogin(t *testing.T) {
	h, cookie := newAuthedHandler(t)

	calls := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/info", ""},
		{http.MethodGet, "/api/notes", ""},
		{http.MethodPost, "/api/notes", `{"name":"x"}`},
		{http.MethodGet, "/api/notes/x.txt", ""},
		{http.MethodPut, "/api/notes/x.txt", `{"content":"y"}`},
		{http.MethodDelete, "/api/notes/x.txt", ""},
		{http.MethodPost, "/api/notes/x.txt/rename", `{"name":"z"}`},
		// Missing "next" is a 400 while logged in, but a 401 without a cookie.
		{http.MethodPost, "/api/password", `{"current":"a"}`},
	}
	for _, c := range calls {
		if rec := do(t, h, c.method, c.path, c.body); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without login = %d, want 401", c.method, c.path, rec.Code)
		}
		if rec := doWith(t, h, cookie, c.method, c.path, c.body); rec.Code == http.StatusUnauthorized {
			t.Errorf("%s %s with login = 401", c.method, c.path)
		}
	}
}

func TestFirstRunSetupAndLogin(t *testing.T) {
	dir := t.TempDir()
	h, _ := newHandler(t, dir, "")

	// Before any account exists the front-end is told to show the setup form.
	rec := do(t, h, http.MethodGet, "/api/session", "")
	var session struct {
		NeedsSetup    bool   `json:"needsSetup"`
		Authenticated bool   `json:"authenticated"`
		Username      string `json:"username"`
	}
	mustJSON(t, rec.Body.Bytes(), &session)
	if !session.NeedsSetup || session.Authenticated {
		t.Fatalf("fresh session = %+v", session)
	}
	if rec := do(t, h, http.MethodPost, "/api/login", `{"username":"admin","password":"secret123"}`); rec.Code != http.StatusConflict {
		t.Fatalf("login before setup = %d, want 409", rec.Code)
	}

	// Weak and malformed accounts are refused.
	if rec := do(t, h, http.MethodPost, "/api/setup", `{"username":"admin","password":"123"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("short password status = %d, want 400", rec.Code)
	}
	if rec := do(t, h, http.MethodPost, "/api/setup", `{"username":"../etc","password":"secret123"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad username status = %d, want 400", rec.Code)
	}
	if rec := do(t, h, http.MethodPost, "/api/setup", `{"username":"","password":"secret123"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty username status = %d, want 400", rec.Code)
	}

	// First account: created and logged in straight away.
	rec = do(t, h, http.MethodPost, "/api/setup", `{"username":"admin","password":"secret123","remember":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup status = %d body=%s", rec.Code, rec.Body.String())
	}
	cookie := sessionCookie(t, rec)
	if cookie.Path != "/" {
		t.Fatalf("cookie path = %q, want /", cookie.Path)
	}
	if !cookie.HttpOnly {
		t.Fatal("session cookie should be HttpOnly")
	}
	if cookie.MaxAge <= 0 {
		t.Fatalf("remember cookie MaxAge = %d, want positive", cookie.MaxAge)
	}

	// A second setup attempt must never create another account.
	if rec := do(t, h, http.MethodPost, "/api/setup", `{"username":"other","password":"secret123"}`); rec.Code != http.StatusConflict {
		t.Fatalf("second setup status = %d, want 409", rec.Code)
	}

	rec = doWith(t, h, cookie, http.MethodGet, "/api/session", "")
	mustJSON(t, rec.Body.Bytes(), &session)
	if session.NeedsSetup || !session.Authenticated || session.Username != testUser {
		t.Fatalf("session after setup = %+v", session)
	}

	// Wrong password is rejected, right password gets a session cookie.
	if rec := do(t, h, http.MethodPost, "/api/login", `{"username":"admin","password":"nope"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d, want 401", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/api/login", `{"username":"Admin","password":"secret123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	if loginCookie := sessionCookie(t, rec); loginCookie.MaxAge != 0 {
		t.Fatalf("plain login cookie should be a session cookie, MaxAge = %d", loginCookie.MaxAge)
	}

	// The password is never stored in the clear, and the file is private.
	path := filepath.Join(dir, "users.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret123") {
		t.Fatalf("users.json leaks the password: %s", raw)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("users.json mode = %o, want 600", perm)
	}
}

func TestChangePasswordAndLogout(t *testing.T) {
	h, cookie := newAuthedHandler(t)

	// A second device logs in; changing the password on the first one must
	// kick the second one out.
	rec := do(t, h, http.MethodPost, "/api/login", `{"username":"`+testUser+`","password":"`+testPass+`"}`)
	otherCookie := sessionCookie(t, rec)

	if rec := doWith(t, h, cookie, http.MethodPost, "/api/password",
		`{"current":"wrong","next":"newsecret1"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password status = %d, want 401", rec.Code)
	}
	if rec := doWith(t, h, cookie, http.MethodPost, "/api/password",
		`{"current":"`+testPass+`","next":"short"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("weak new password status = %d, want 400", rec.Code)
	}
	rec = doWith(t, h, cookie, http.MethodPost, "/api/password",
		`{"current":"`+testPass+`","next":"newsecret1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("change password status = %d body=%s", rec.Code, rec.Body.String())
	}

	// The caller keeps its session, the other device does not.
	if rec := doWith(t, h, cookie, http.MethodGet, "/api/notes", ""); rec.Code != 200 {
		t.Fatalf("own session after change = %d, want 200", rec.Code)
	}
	if rec := doWith(t, h, otherCookie, http.MethodGet, "/api/notes", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("other session after change = %d, want 401", rec.Code)
	}

	// Old password is gone, the new one works.
	if rec := do(t, h, http.MethodPost, "/api/login", `{"username":"`+testUser+`","password":"`+testPass+`"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("login with old password = %d, want 401", rec.Code)
	}
	if rec := do(t, h, http.MethodPost, "/api/login", `{"username":"`+testUser+`","password":"newsecret1"}`); rec.Code != http.StatusOK {
		t.Fatalf("login with new password = %d", rec.Code)
	}

	// Logout clears both the session and the cookie on the client.
	rec = doWith(t, h, cookie, http.MethodPost, "/api/logout", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d", rec.Code)
	}
	cleared := sessionCookie(t, rec)
	if cleared.MaxAge >= 0 {
		t.Fatalf("logout cookie MaxAge = %d, want negative", cleared.MaxAge)
	}
	if rec := doWith(t, h, cookie, http.MethodGet, "/api/notes", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("notes after logout = %d, want 401", rec.Code)
	}
}

func TestOperationLogs(t *testing.T) {
	h, cookie := newAuthedHandler(t)

	// 未登录不能看日志。
	if rec := do(t, h, http.MethodGet, "/api/logs", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous logs status = %d, want 401", rec.Code)
	}

	// 新建 → 重命名 → 删除，每一步都会写一条日志。
	if rec := doWith(t, h, cookie, http.MethodPost, "/api/notes", `{"name":"日志测试"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doWith(t, h, cookie, http.MethodPost, "/api/notes/日志测试.txt/rename", `{"name":"改名"}`); rec.Code != 200 {
		t.Fatalf("rename status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doWith(t, h, cookie, http.MethodDelete, "/api/notes/改名.txt", ""); rec.Code != 200 {
		t.Fatalf("delete status = %d", rec.Code)
	}

	rec := doWith(t, h, cookie, http.MethodGet, "/api/logs", "")
	if rec.Code != 200 {
		t.Fatalf("logs status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Logs []oplog.Entry `json:"logs"`
	}
	mustJSON(t, rec.Body.Bytes(), &body)
	if len(body.Logs) == 0 {
		t.Fatal("expected at least one log entry")
	}
	// 最新的操作（删除笔记）在最前面。
	if body.Logs[0].Action != "删除笔记" || body.Logs[0].Detail != "改名.txt" {
		t.Fatalf("newest entry = %+v", body.Logs[0])
	}
}

func TestLoginThrottledAfterRepeatedFailures(t *testing.T) {
	h, _ := newAuthedHandler(t)

	for i := 0; i < maxLoginFailures; i++ {
		if rec := do(t, h, http.MethodPost, "/api/login", `{"username":"admin","password":"nope"}`); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i+1, rec.Code)
		}
	}
	rec := do(t, h, http.MethodPost, "/api/login", `{"username":"admin","password":"nope"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("throttled status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("throttled response should carry Retry-After")
	}
	// Even the right password has to wait out the cool-down.
	if rec := do(t, h, http.MethodPost, "/api/login", `{"username":"admin","password":"`+testPass+`"}`); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("correct password while throttled = %d, want 429", rec.Code)
	}
	// The counter is per username, so other accounts are unaffected.
	if rec := do(t, h, http.MethodPost, "/api/login", `{"username":"someone-else","password":"nope"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("other username status = %d, want 401", rec.Code)
	}
}

func TestServesIndexAndAssets(t *testing.T) {
	h := newTestHandler(t)

	rec := do(t, h, http.MethodGet, "/", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "记事本") {
		t.Fatalf("index status = %d", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/assets/app.css", ""); rec.Code != 200 {
		t.Fatalf("css status = %d", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/assets/app.js", ""); rec.Code != 200 {
		t.Fatalf("js status = %d", rec.Code)
	}
}

// The fnOS gateway proxies /app/notepad/ to the app; the same handler must
// work both under that prefix and when addressed directly.
func TestGatewayPrefix(t *testing.T) {
	h, _ := newHandler(t, t.TempDir(), "/app/notepad")

	rec := do(t, h, http.MethodGet, "/app/notepad/", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "记事本") {
		t.Fatalf("gateway index status = %d", rec.Code)
	}

	// Hitting the prefix without a trailing slash must redirect to the
	// directory form, otherwise relative asset URLs resolve outside the app.
	if rec := do(t, h, http.MethodGet, "/app/notepad", ""); rec.Code != http.StatusMovedPermanently {
		t.Fatalf("gateway no-slash status = %d", rec.Code)
	} else if loc := rec.Header().Get("Location"); loc != "/app/notepad/" {
		t.Fatalf("gateway no-slash Location = %q", loc)
	}
	if !strings.Contains(rec.Body.String(), `href="assets/app.css"`) {
		t.Fatalf("index should use relative asset URLs")
	}
	if rec := do(t, h, http.MethodGet, "/app/notepad/assets/app.css", ""); rec.Code != 200 {
		t.Fatalf("gateway asset status = %d", rec.Code)
	}

	// Anonymous calls through the gateway are refused too.
	if rec := do(t, h, http.MethodGet, "/app/notepad/api/notes", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("gateway anonymous api status = %d, want 401", rec.Code)
	}

	// Logging in through the gateway scopes the cookie to the app path, so
	// other apps served by the same NAS host never see it.
	rec = do(t, h, http.MethodPost, "/app/notepad/api/setup",
		`{"username":"`+testUser+`","password":"`+testPass+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("gateway setup status = %d body=%s", rec.Code, rec.Body.String())
	}
	cookie := sessionCookie(t, rec)
	if cookie.Path != "/app/notepad/" {
		t.Fatalf("gateway cookie path = %q, want /app/notepad/", cookie.Path)
	}
	if rec := doWith(t, h, cookie, http.MethodGet, "/app/notepad/api/info", ""); rec.Code != 200 {
		t.Fatalf("gateway api status = %d", rec.Code)
	}

	// Direct access on the service port keeps working unchanged, with its own
	// (root-scoped) cookie.
	rec = do(t, h, http.MethodPost, "/api/login", `{"username":"`+testUser+`","password":"`+testPass+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("direct login status = %d body=%s", rec.Code, rec.Body.String())
	}
	directCookie := sessionCookie(t, rec)
	if directCookie.Path != "/" {
		t.Fatalf("direct cookie path = %q, want /", directCookie.Path)
	}
	if rec := doWith(t, h, directCookie, http.MethodGet, "/api/info", ""); rec.Code != 200 {
		t.Fatalf("direct api status = %d", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/assets/app.css", ""); rec.Code != 200 {
		t.Fatalf("direct asset status = %d", rec.Code)
	}
}

func mustJSON(t *testing.T, data []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}
}
