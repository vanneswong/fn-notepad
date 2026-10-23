// Package server wires the note store to a small JSON HTTP API and serves the
// embedded single-page front-end.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vanneswong/fnos-notepad/internal/auth"
	"github.com/vanneswong/fnos-notepad/internal/oplog"
	"github.com/vanneswong/fnos-notepad/internal/store"
	"github.com/vanneswong/fnos-notepad/web"
)

// Login throttling: five wrong passwords within five minutes per client and
// username, then a cool-down.
const (
	maxLoginFailures = 5
	loginFailWindow  = 5 * time.Minute
)

// Config carries the small amount of state the HTTP layer needs.
type Config struct {
	Name    string
	Version string
	Store   *store.Store
	// Auth holds the accounts. It must not be nil.
	Auth *auth.Store
	// Logs is an optional operation log. When nil, operation logging is
	// disabled (used by tests and simple embedders).
	Logs *oplog.Log
	// Prefix is an optional gateway path prefix (for example "/app/notepad")
	// that is stripped before routing, so the same handler works both when the
	// app is reached directly and when nginx proxies it under a sub-path.
	Prefix string
}

// ctxKey is the private context key type for values set by this package.
type ctxKey int

// cookieScopeKey remembers the gateway prefix a request came in through, so
// the session cookie can be scoped to "/app/notepad/" only when the browser is
// actually talking to the gateway.
const cookieScopeKey ctxKey = 0

// New returns an http.Handler serving the API and the web UI.
func New(cfg Config) http.Handler {
	if cfg.Auth == nil {
		panic("server: Config.Auth must not be nil")
	}
	mux := http.NewServeMux()
	api := &api{
		cfg:      cfg,
		sessions: auth.NewSessions(),
		limiter:  auth.NewLimiter(maxLoginFailures, loginFailWindow),
	}

	// Public: everything a visitor needs before they are logged in.
	mux.HandleFunc("GET /api/session", api.sessionInfo)
	mux.HandleFunc("POST /api/setup", api.setup)
	mux.HandleFunc("POST /api/login", api.login)
	mux.HandleFunc("POST /api/logout", api.logout)

	// Everything below requires a valid session cookie.
	mux.HandleFunc("GET /api/info", api.requireAuth(api.info))
	mux.HandleFunc("GET /api/notes", api.requireAuth(api.listNotes))
	mux.HandleFunc("POST /api/notes", api.requireAuth(api.createNote))
	mux.HandleFunc("GET /api/notes/{name}", api.requireAuth(api.readNote))
	mux.HandleFunc("PUT /api/notes/{name}", api.requireAuth(api.writeNote))
	mux.HandleFunc("DELETE /api/notes/{name}", api.requireAuth(api.deleteNote))
	mux.HandleFunc("POST /api/notes/{name}/rename", api.requireAuth(api.renameNote))
	mux.HandleFunc("POST /api/password", api.requireAuth(api.changePassword))
	mux.HandleFunc("GET /api/logs", api.requireAuth(api.listLogs))

	mux.Handle("GET /assets/", http.StripPrefix("/assets/", noCache(http.FileServerFS(assetsFS()))))
	mux.HandleFunc("GET /", serveIndex)

	return secureHeaders(recoverer(logger(stripPrefix(cfg.Prefix, mux))))
}

// stripPrefix removes a leading gateway prefix from request paths. It is a
// no-op for requests that do not carry the prefix, so direct access on the
// service port keeps working unchanged.
func stripPrefix(prefix string, next http.Handler) http.Handler {
	prefix = "/" + strings.Trim(prefix, "/")
	if prefix == "/" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch p := r.URL.Path; {
		case p == prefix:
			// A gateway entry may point at "/app/notepad" without the trailing
			// slash. Redirect so the document base URL is a directory and the
			// relative asset paths ("assets/…") resolve under the prefix instead
			// of one level up. Without this the page loads blank.
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				u := *r.URL
				u.Path = prefix + "/"
				http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
				return
			}
			r.URL.Path = "/"
		case strings.HasPrefix(p, prefix+"/"):
			r.URL.Path = strings.TrimPrefix(p, prefix)
			// Remember where the request entered the app: the session cookie
			// is scoped to the gateway path when the browser used the gateway,
			// and to "/" when the app was reached on its own port.
			r = r.WithContext(context.WithValue(r.Context(), cookieScopeKey, prefix))
		}
		next.ServeHTTP(w, r)
	})
}

// assetsFS exposes just the "assets" subtree of the embedded web files.
func assetsFS() fs.FS {
	sub, err := fs.Sub(web.FS, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(web.FS, "index.html")
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

type api struct {
	cfg      Config
	sessions *auth.Sessions
	limiter  *auth.Limiter
}

// ---- auth ----------------------------------------------------------------

// sessionInfo tells the front-end whether it has to show the first-run account
// form, the login screen, or the app itself.
func (a *api) sessionInfo(w http.ResponseWriter, r *http.Request) {
	needsSetup := !a.cfg.Auth.Configured()
	username, ok := a.currentUser(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"needsSetup":    needsSetup,
		"authenticated": ok,
		"username":      username,
	})
}

// setup creates the very first account. It only works while no account exists,
// which is what makes "首次安装需要创建新用户" safe against remote abuse: once
// a device is set up, the endpoint can never create another account.
func (a *api) setup(w http.ResponseWriter, r *http.Request) {
	if a.cfg.Auth.Configured() {
		writeError(w, http.StatusConflict, auth.ErrAlreadySetup.Error())
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, err := a.cfg.Auth.Create(body.Username, body.Password)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	a.startSession(w, r, user.Username, body.Remember)
	log.Printf("创建账号 %q（用户文件 %s）", user.Username, a.cfg.Auth.Path())
	a.log("创建账号", user.Username, "")
	writeJSON(w, http.StatusCreated, map[string]any{"username": user.Username, "created": true})
}

func (a *api) login(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.Auth.Configured() {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":      auth.ErrNoUsers.Error() + "，请先创建账号",
			"needsSetup": true,
		})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	key := clientKey(r) + "|" + strings.ToLower(strings.TrimSpace(body.Username))
	if wait := a.limiter.RetryAfter(key); wait > 0 {
		seconds := int(wait.Seconds()) + 1
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeError(w, http.StatusTooManyRequests,
			"密码错误次数过多，请 "+strconv.Itoa(seconds)+" 秒后再试")
		return
	}

	user, err := a.cfg.Auth.Authenticate(body.Username, body.Password)
	if err != nil {
		a.limiter.Fail(key)
		writeAuthError(w, err)
		return
	}
	a.limiter.Reset(key)
	a.startSession(w, r, user.Username, body.Remember)
	a.log("登录", user.Username, "")
	writeJSON(w, http.StatusOK, map[string]any{"username": user.Username})
}

func (a *api) logout(w http.ResponseWriter, r *http.Request) {
	username, _ := a.currentUser(r)
	if c, err := r.Cookie(auth.CookieName); err == nil {
		a.sessions.Stop(c.Value)
	}
	a.clearSession(r, w)
	if username != "" {
		a.log("退出登录", username, "")
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// changePassword swaps the password of the logged-in user. Every other session
// of that user is dropped; the caller keeps its own.
func (a *api) changePassword(w http.ResponseWriter, r *http.Request) {
	username, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "请先登录")
		return
	}
	var body struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.cfg.Auth.ChangePassword(username, body.Current, body.Next); err != nil {
		writeAuthError(w, err)
		return
	}
	if c, err := r.Cookie(auth.CookieName); err == nil {
		a.sessions.StopUser(username, c.Value)
	}
	log.Printf("账号 %q 修改了密码", username)
	a.log("修改密码", username, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// requireAuth guards the note API: without a valid session cookie the caller
// gets 401 and the front-end falls back to the login screen.
func (a *api) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.currentUser(r); !ok {
			msg := "请先登录"
			if !a.cfg.Auth.Configured() {
				msg = "尚未创建账号，请先创建"
			}
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error":      msg,
				"needsSetup": !a.cfg.Auth.Configured(),
			})
			return
		}
		next(w, r)
	}
}

func (a *api) currentUser(r *http.Request) (string, bool) {
	c, err := r.Cookie(auth.CookieName)
	if err != nil {
		return "", false
	}
	return a.sessions.Lookup(c.Value)
}

func (a *api) startSession(w http.ResponseWriter, r *http.Request, username string, remember bool) {
	token, expires := a.sessions.Start(username, remember)
	cookie := &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     cookiePath(r, a.cfg.Prefix),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	}
	if remember {
		cookie.Expires = expires
		cookie.MaxAge = int(time.Until(expires).Seconds())
	}
	http.SetCookie(w, cookie)
}

func (a *api) clearSession(r *http.Request, w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     cookiePath(r, a.cfg.Prefix),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
		MaxAge:   -1,
	})
}

// cookiePath scopes the cookie to the path the browser used: "/app/notepad/"
// behind the fnOS gateway, "/" when the app is opened on its own port.
func cookiePath(r *http.Request, prefix string) string {
	if scoped, ok := r.Context().Value(cookieScopeKey).(string); ok && scoped != "" {
		return "/" + strings.Trim(scoped, "/") + "/"
	}
	return "/"
}

// clientKey identifies the caller for throttling. Behind the gateway every
// request arrives through the same unix socket, so this degrades to a global
// per-username limit — still enough to stop password guessing.
func clientKey(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	if host == "" {
		host = "unix"
	}
	return host
}

func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrBadCredentials), errors.Is(err, auth.ErrWrongPassword):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, auth.ErrNoUsers):
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "needsSetup": true})
	case errors.Is(err, auth.ErrInvalidUsername), errors.Is(err, auth.ErrWeakPassword),
		errors.Is(err, auth.ErrPasswordTooLong), errors.Is(err, auth.ErrSamePassword),
		errors.Is(err, auth.ErrAlreadySetup):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, auth.ErrUsernameTaken):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func (a *api) info(w http.ResponseWriter, r *http.Request) {
	notes, err := a.cfg.Store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	username, _ := a.currentUser(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    a.cfg.Name,
		"version": a.cfg.Version,
		"dataDir": a.cfg.Store.Dir(),
		"user":    username,
		"count":   len(notes),
	})
}

func (a *api) listNotes(w http.ResponseWriter, r *http.Request) {
	notes, err := a.cfg.Store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": notes})
}

// listLogs returns the most recent operation records, newest first. It needs
// a valid session like every other /api/* route.
func (a *api) listLogs(w http.ResponseWriter, r *http.Request) {
	if a.cfg.Logs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"logs": []oplog.Entry{}})
		return
	}
	entries, err := a.cfg.Logs.List(200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": entries})
}

func (a *api) createNote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	note, err := a.cfg.Store.Create(body.Name)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if body.Content != "" {
		if note, err = a.cfg.Store.Write(note.Name, body.Content); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	username, _ := a.currentUser(r)
	a.log("新建笔记", username, note.Name)
	writeJSON(w, http.StatusCreated, map[string]any{"note": note})
}

func (a *api) readNote(w http.ResponseWriter, r *http.Request) {
	note, content, err := a.cfg.Store.Read(r.PathValue("name"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"note": note, "content": content})
}

func (a *api) writeNote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content *string `json:"content"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Content == nil {
		writeError(w, http.StatusBadRequest, "missing \"content\" field")
		return
	}
	note, err := a.cfg.Store.Write(r.PathValue("name"), *body.Content)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"note": note})
}

func (a *api) deleteNote(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := a.cfg.Store.Delete(name); err != nil {
		writeStoreError(w, err)
		return
	}
	username, _ := a.currentUser(r)
	a.log("删除笔记", username, name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *api) renameNote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	oldName := r.PathValue("name")
	note, err := a.cfg.Store.Rename(oldName, body.Name)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	username, _ := a.currentUser(r)
	a.log("重命名笔记", username, oldName+" → "+note.Name)
	writeJSON(w, http.StatusOK, map[string]any{"note": note})
}

// ---- helpers -------------------------------------------------------------

// log appends one operation record. It is a no-op when no log is configured.
func (a *api) log(action, user, detail string) {
	if a.cfg.Logs == nil {
		return
	}
	if err := a.cfg.Logs.Append(action, user, detail); err != nil {
		log.Printf("写入操作日志失败: %v", err)
	}
}

func decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid JSON body: " + err.Error())
	}
	return nil
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrInvalidName):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// secureHeaders sets a conservative set of response headers. Frame embedding is
// left open on purpose so the app can also be shown inside a NAS dashboard.
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self'; "+
				"script-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'")
		next.ServeHTTP(w, r)
	})
}

// ---- middleware ----------------------------------------------------------

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
		}
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, rec)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
