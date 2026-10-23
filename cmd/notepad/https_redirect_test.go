package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPSRedirectByHost(t *testing.T) {
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	h := httpsRedirect(base, ":18181")

	cases := []struct {
		name     string
		host     string
		path     string
		wantCode int
		wantLoc  string
	}{
		{"ipv4 allows http", "192.168.10.232:18180", "/", http.StatusOK, ""},
		{"ipv4 loopback allows http", "127.0.0.1:18180", "/", http.StatusOK, ""},
		{"ipv6 loopback allows http", "[::1]:18180", "/", http.StatusOK, ""},
		{"domain redirects to https", "fn.myminibase.com:18180", "/note?x=1",
			http.StatusMovedPermanently, "https://fn.myminibase.com:18181/note?x=1"},
		{"localhost redirects to https", "localhost:18180", "/",
			http.StatusMovedPermanently, "https://localhost:18181/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://"+tc.host+tc.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if loc := rec.Header().Get("Location"); loc != tc.wantLoc {
				t.Fatalf("Location = %q, want %q", loc, tc.wantLoc)
			}
		})
	}
}

func TestHTTPSRedirectDisabledWithoutTLS(t *testing.T) {
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	h := httpsRedirect(base, "")

	req := httptest.NewRequest(http.MethodGet, "http://fn.myminibase.com:18180/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200（未配置 TLS 时不应重定向）", rec.Code)
	}
	if rec.Header().Get("Location") != "" {
		t.Fatalf("unexpected Location %q", rec.Header().Get("Location"))
	}
}

func TestHTTPSPort(t *testing.T) {
	cases := map[string]string{
		"":              "",
		":18181":        "18181",
		"0.0.0.0:18181": "18181",
		"[::]:18181":    "18181",
		"18181":         "18181",
	}
	for in, want := range cases {
		if got := httpsPort(in); got != want {
			t.Errorf("httpsPort(%q) = %q, want %q", in, got, want)
		}
	}
}
