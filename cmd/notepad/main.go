// Command notepad serves the 记事本 web app: a tiny, dependency-free HTTP
// server that reads and writes plain-text notes from a single directory.
//
// It can serve plain HTTP, HTTPS, or both at once, which is what lets the app
// be reached as http://host:18180/ (LAN) and https://host:18181/ (through the
// NAS reverse proxy) at the same time.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/vanneswong/fnos-notepad/internal/auth"
	"github.com/vanneswong/fnos-notepad/internal/oplog"
	"github.com/vanneswong/fnos-notepad/internal/server"
	"github.com/vanneswong/fnos-notepad/internal/store"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

const appName = "记事本"

func main() {
	var (
		addr    = flag.String("addr", envOr("NOTEPAD_ADDR", ":18180"), "HTTP listen address (empty disables)")
		unixSck = flag.String("unix", envOr("NOTEPAD_UNIX", ""), "unix socket path for the fnOS gateway (empty disables)")
		tlsAddr = flag.String("tls-addr", envOr("NOTEPAD_TLS_ADDR", ""), "HTTPS listen address (empty disables)")
		tlsCert = flag.String("tls-cert", envOr("NOTEPAD_TLS_CERT", ""), "TLS certificate file (PEM)")
		tlsKey  = flag.String("tls-key", envOr("NOTEPAD_TLS_KEY", ""), "TLS private key file (PEM)")
		prefix  = flag.String("prefix", envOr("NOTEPAD_PREFIX", "/app/notepad"), "gateway path prefix to strip")
		dataDir = flag.String("data", envOr("NOTEPAD_DATA", defaultDataDir()), "directory that holds the notes")
		users   = flag.String("users", envOr("NOTEPAD_USERS", ""), "user database file (default: <data>/../users.json)")
		showVer = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("%s %s\n", appName, version)
		return
	}

	st, err := store.New(*dataDir)
	if err != nil {
		log.Fatalf("cannot open notes directory %q: %v", *dataDir, err)
	}

	// The user database lives next to the notes directory so that both sit in
	// ${TRIM_PKGVAR} and survive upgrades (and uninstalls, like the notes).
	usersPath := *users
	if usersPath == "" {
		usersPath = filepath.Join(filepath.Dir(*dataDir), "users.json")
	}
	accounts, err := auth.New(usersPath)
	if err != nil {
		log.Fatalf("cannot open user database %q: %v", usersPath, err)
	}

	// 操作日志与 users.json 一样放在 notes 目录的上一级（fnOS 下即
	// ${TRIM_PKGVAR}），升级、卸载都会保留。
	opLog, err := oplog.New(filepath.Join(filepath.Dir(*dataDir), "oplog.jsonl"))
	if err != nil {
		log.Fatalf("cannot open operation log: %v", err)
	}

	handler := server.New(server.Config{
		Name:    appName,
		Version: version,
		Store:   st,
		Auth:    accounts,
		Logs:    opLog,
		Prefix:  *prefix,
	})

	log.Printf("%s %s", appName, version)
	log.Printf("notes directory: %s", st.Dir())
	log.Printf("user database:   %s", accounts.Path())
	log.Printf("operation log:   %s", opLog.Path())
	if !accounts.Configured() {
		log.Printf("首次运行：请打开界面创建账号（第一个账号创建后，创建入口自动关闭）")
	}

	var servers []*http.Server

	if *addr != "" {
		srv := newServer(handler)
		ln, err := net.Listen("tcp", *addr)
		if err != nil {
			log.Fatalf("cannot listen on %s: %v", *addr, err)
		}
		servers = append(servers, srv)
		go serve(srv, ln, "", "")
		log.Printf("http  listening on http://%s", ln.Addr())
	}

	if *tlsAddr != "" {
		switch {
		case *tlsCert == "" || *tlsKey == "":
			log.Printf("https disabled: no certificate configured (set -tls-cert / -tls-key)")
		default:
			srv := newServer(handler)
			ln, err := net.Listen("tcp", *tlsAddr)
			if err != nil {
				log.Fatalf("cannot listen on %s: %v", *tlsAddr, err)
			}
			servers = append(servers, srv)
			go serve(srv, ln, *tlsCert, *tlsKey)
			log.Printf("https listening on https://%s", ln.Addr())
		}
	}

	if *unixSck != "" {
		srv := newServer(handler)
		// A stale socket from a previous run would make Listen fail.
		_ = os.Remove(*unixSck)
		ln, err := net.Listen("unix", *unixSck)
		if err != nil {
			log.Fatalf("cannot listen on unix socket %s: %v", *unixSck, err)
		}
		// nginx workers usually run as a different user, so the socket has to be
		// reachable by anyone who can already read the app directory.
		if err := os.Chmod(*unixSck, 0o666); err != nil {
			log.Printf("cannot chmod %s: %v", *unixSck, err)
		}
		servers = append(servers, srv)
		go serve(srv, ln, "", "")
		log.Printf("unix  listening on %s", *unixSck)
	}

	if len(servers) == 0 {
		log.Fatalf("nothing to do: -addr, -tls-addr and -unix are all empty")
	}

	// Wait for SIGINT/SIGTERM, then shut every listener down cleanly.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Printf("shutting down …")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}
	if *unixSck != "" {
		_ = os.Remove(*unixSck)
	}
}

func newServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

func serve(srv *http.Server, ln net.Listener, certFile, keyFile string) {
	var err error
	if certFile != "" && keyFile != "" {
		err = srv.ServeTLS(ln, certFile, keyFile)
	} else {
		err = srv.Serve(ln)
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
}

// defaultDataDir picks a sensible notes directory with a graceful fallback so
// the binary also runs fine when tested by hand outside the package manager.
func defaultDataDir() string {
	if v := os.Getenv("NOTEPAD_HOME"); v != "" {
		return filepath.Join(v, "notes")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".notepad", "notes")
	}
	return filepath.Join(os.TempDir(), "notepad-notes")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
