package tracking

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPublicIPRefusesEveryNonPublicRange(t *testing.T) {
	for ip, ok := range map[string]bool{
		"8.8.8.8": true, "1.1.1.1": true, "113.96.1.1": true, "2606:4700:4700::1111": true,
		"127.0.0.1": false, "127.255.255.254": false, "::1": false, "10.0.0.1": false, "172.16.5.4": false, "172.31.255.255": false, "192.168.1.1": false,
		"169.254.169.254": false, "fe80::1": false, "fc00::1": false, "fd12:3456::1": false, "100.64.0.1": false, "100.127.255.255": false,
		"0.0.0.0": false, "::": false, "224.0.0.1": false, "ff02::1": false, "240.0.0.1": false, "255.255.255.255": false,
		"::ffff:127.0.0.1": false, "::ffff:10.1.2.3": false, "::ffff:169.254.169.254": false, "::ffff:8.8.8.8": true,
		"::ffff:100.64.0.1": false, "::ffff:192.0.2.1": false, "::ffff:198.18.0.9": false, "::ffff:240.0.0.1": false,
		"192.0.2.10": false, "198.51.100.7": false, "203.0.113.9": false, "198.18.0.1": false, "64:ff9b::7f00:1": false, "2001:db8::1": false,
	} {
		if got := publicIP(netip.MustParseAddr(ip)); got != ok {
			t.Errorf("%s: got %v want %v", ip, got, ok)
		}
	}
}

func permissiveFetcher() *SafeMediaFetcher {
	f := NewSafeMediaFetcher()
	f.validate = func(*url.URL) error { return nil }
	return f
}

// The connection check is the real SSRF defence: even when the URL passes, the
// production dialer refuses to connect to a loopback server.
func TestSafeMediaFetcherRefusesToConnectToInternalAddresses(t *testing.T) {
	var hits atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("secret"))
	}))
	defer internal.Close()
	if _, err := permissiveFetcher().Fetch(context.Background(), internal.URL+"/x.png", 1<<20); err == nil || hits.Load() != 0 {
		t.Fatalf("connected to a loopback server (hits=%d, err=%v)", hits.Load(), err)
	}
}

// The URL policy is tested directly: refusing a request because the network is
// unreachable would pass for the wrong reason.
func TestSafeMediaFetcherURLPolicy(t *testing.T) {
	f := NewSafeMediaFetcher()
	for raw, ok := range map[string]bool{
		"https://mmbiz.qpic.cn/mmbiz_png/a/0?wx_fmt=png": true, "https://thirdwx.qlogo.cn/a/132": true, "https://mmbiz.qpic.cn:443/a": true,
		"http://mmbiz.qpic.cn/a": false, "https://evil.example/a.png": false, "https://mmbiz.qpic.cn.evil.example/a": false, "https://evilqpic.cn/a": false,
		"https://user:pw@mmbiz.qpic.cn/a": false, "https://mmbiz.qpic.cn:8443/a": false, "https://127.0.0.1/a": false, "https://[::1]/a": false,
		"https://localhost/a": false, "file:///etc/passwd": false, "ftp://mmbiz.qpic.cn/a": false, "gopher://mmbiz.qpic.cn/a": false, "//mmbiz.qpic.cn/a": false,
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if got := f.validate(u) == nil; got != ok {
			t.Errorf("%q: allowed=%v want %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"", "::bad", "https://evil.example/a.png"} {
		if _, err := f.Fetch(context.Background(), raw, 1<<20); err == nil {
			t.Errorf("Fetch accepted %q", raw)
		}
	}
	if _, err := f.Fetch(context.Background(), "https://mmbiz.qpic.cn/a", 0); err == nil {
		t.Error("zero size limit accepted")
	}
}

// Behaviour once a connection is allowed: bounded size, content type, status and
// no redirect following. A test client replaces the transport only so the local
// server is reachable; the policy under test is the response handling.
func localFetcher() *SafeMediaFetcher {
	f := permissiveFetcher()
	f.client.Transport = http.DefaultTransport
	return f
}

func TestSafeMediaFetcherResponseHandling(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte("a"), 100)...)
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("credentials sent: %v", r.Header)
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	})
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(bytes.Repeat([]byte("b"), 5000))
	})
	mux.HandleFunc("/html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>"))
	})
	mux.HandleFunc("/missing", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "image/png") })
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) })
	server := httptest.NewServer(mux)
	defer server.Close()
	f := localFetcher()
	got, err := f.Fetch(context.Background(), server.URL+"/ok", 1000)
	if err != nil || !bytes.Equal(got, png) {
		t.Fatalf("valid picture: %v", err)
	}
	for path, limit := range map[string]int64{"/big": 1000, "/html": 1000, "/missing": 1000, "/empty": 1000, "/redirect": 1000, "/ok": 50} {
		if data, err := f.Fetch(context.Background(), server.URL+path, limit); err == nil {
			t.Errorf("%s accepted (%d bytes)", path, len(data))
		}
	}
	if redirected.Load() != 0 {
		t.Fatal("a redirect was followed")
	}
	if !strings.Contains(server.URL, "127.0.0.1") {
		t.Fatal("test server is not local")
	}
}
