package scrapling

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dorukardahan/nole/internal/safenet"
)

var errBrowserNetwork = errors.New("scrapling: browser connection blocked or unavailable")

// browserTunnelHandshakeTimeout bounds TLS and CONNECT with an upstream proxy.
var browserTunnelHandshakeTimeout = 15 * time.Second

// browserDialer resolves once, validates every answer, and connects only to a
// validated literal. An upstream proxy receives that literal too, never the
// target hostname. Chromium retains the original hostname for Host and TLS.
type browserDialer struct {
	lookup   func(context.Context, string) ([]net.IP, error)
	dial     func(context.Context, string, string) (net.Conn, error)
	upstream *url.URL
}

func (d browserDialer) connect(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || strings.Contains(host, "%") || safenet.IsBlockedHost(host) {
		return nil, errBrowserNetwork
	}
	ips := []net.IP{net.ParseIP(host)}
	if ips[0] == nil {
		ips, err = d.lookup(ctx, host)
	}
	if err != nil || len(ips) == 0 {
		return nil, errBrowserNetwork
	}
	for _, ip := range ips {
		if ip == nil || safenet.ValidateIP(ip) != nil {
			return nil, errBrowserNetwork
		}
	}
	for _, ip := range ips {
		pinned := net.JoinHostPort(ip.String(), port)
		var conn net.Conn
		if d.upstream == nil {
			conn, err = d.dial(ctx, "tcp", pinned)
		} else {
			conn, err = d.tunnel(ctx, pinned)
		}
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errBrowserNetwork
}

func (d browserDialer) tunnel(ctx context.Context, pinned string) (net.Conn, error) {
	u := d.upstream
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	// The upstream endpoint is explicit operator configuration and may be local.
	c, err := d.dial(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return nil, errBrowserNetwork
	}
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	raw := c
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	// Never wait on an unresponsive upstream longer than the handshake bound,
	// even when the caller's context carries no deadline.
	deadline := time.Now().Add(browserTunnelHandshakeTimeout)
	if ctxDeadline, exists := ctx.Deadline(); exists && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	c.SetDeadline(deadline)
	if u.Scheme == "https" {
		t := tls.Client(c, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
		if t.HandshakeContext(ctx) != nil {
			return nil, errBrowserNetwork
		}
		c = t
	}
	r := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: pinned}, Host: pinned, Header: make(http.Header)}
	if u.User != nil {
		password, _ := u.User.Password()
		r.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+password)))
	}
	if r.Write(c) != nil {
		return nil, errBrowserNetwork
	}
	reader := bufio.NewReader(c)
	resp, err := http.ReadResponse(reader, r)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil, errBrowserNetwork
	}
	c.SetDeadline(time.Time{})
	ok = true
	return &bufferedBrowserConn{Conn: c, reader: reader}, nil
}

type bufferedBrowserConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedBrowserConn) Read(b []byte) (int, error) { return c.reader.Read(b) }

// This listener lives only for one browser session. It is loopback-only,
// authenticated, never logs targets, and ignores ambient proxy variables.
func startBrowserProxy(ctx context.Context, upstream string) (string, func(), error) {
	d := browserDialer{lookup: func(ctx context.Context, host string) ([]net.IP, error) {
		return net.DefaultResolver.LookupIP(ctx, "ip", host)
	}, dial: (&net.Dialer{Timeout: 10 * time.Second}).DialContext}
	if upstream != "" {
		u, err := url.Parse(upstream)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			return "", nil, errors.New("scrapling: browser mode requires an HTTP or HTTPS upstream proxy")
		}
		d.upstream = u
	}
	return listenBrowserProxy(ctx, d)
}

func listenBrowserProxy(ctx context.Context, d browserDialer) (string, func(), error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", nil, errBrowserNetwork
	}
	token := rand.Text()
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("nole:"+token))
	// http.Transport detaches dial contexts from the request, so bind every
	// dial (and any pending upstream handshake) to this proxy's lifetime too.
	proxyCtx, cancelProxy := context.WithCancel(ctx)
	dial := func(dialCtx context.Context, network, address string) (net.Conn, error) {
		dialCtx, cancel := context.WithCancel(dialCtx)
		defer cancel()
		stop := context.AfterFunc(proxyCtx, cancel)
		defer stop()
		return d.connect(dialCtx, network, address)
	}
	transport := &http.Transport{Proxy: nil, DialContext: dial, ForceAttemptHTTP2: false, DisableKeepAlives: true, ResponseHeaderTimeout: 15 * time.Second}
	var mu sync.Mutex
	connections := make(map[net.Conn]bool)
	closed := false
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 32 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Proxy-Authorization")), []byte(auth)) != 1 {
			w.Header().Set("Proxy-Authenticate", `Basic realm="nole"`)
			http.Error(w, "proxy authentication required", 407)
			return
		}
		if r.Method == http.MethodConnect {
			upstream, err := dial(r.Context(), "tcp", r.Host)
			if err != nil {
				http.Error(w, "connection blocked or unavailable", 403)
				return
			}
			defer upstream.Close()
			client, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			defer client.Close()
			mu.Lock()
			if closed {
				mu.Unlock()
				return
			}
			connections[client] = true
			connections[upstream] = true
			mu.Unlock()
			defer func() { mu.Lock(); delete(connections, client); delete(connections, upstream); mu.Unlock() }()
			rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
			if rw.Flush() != nil {
				return
			}
			done := make(chan struct{})
			go func() { io.Copy(upstream, rw); closeBrowserWrite(upstream); close(done) }()
			io.Copy(client, upstream)
			client.Close()
			upstream.Close()
			<-done
			return
		}
		if r.URL.Scheme != "http" || r.URL.Host == "" || r.URL.User != nil || r.Header.Get("Upgrade") != "" {
			http.Error(w, "request blocked", 403)
			return
		}
		if r.Method == http.MethodGet && r.URL.Host == "nole-readiness.invalid" && r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("nole browser proxy ready"))
			return
		}
		out := r.Clone(r.Context())
		out.RequestURI = ""
		out.Host = out.URL.Host
		stripBrowserHopHeaders(out.Header)
		resp, err := transport.RoundTrip(out)
		if err != nil {
			http.Error(w, "connection blocked or unavailable", 403)
			return
		}
		defer resp.Body.Close()
		stripBrowserHopHeaders(resp.Header)
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})
	go server.Serve(listener)
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			cancelProxy()
			mu.Lock()
			closed = true
			for c := range connections {
				c.Close()
			}
			mu.Unlock()
			server.Close()
			transport.CloseIdleConnections()
		})
	}
	stop := context.AfterFunc(ctx, cleanup)
	return "http://nole:" + token + "@" + listener.Addr().String(), func() { stop(); cleanup() }, nil
}

func stripBrowserHopHeaders(h http.Header) {
	for _, value := range h.Values("Connection") {
		for _, key := range strings.Split(value, ",") {
			h.Del(strings.TrimSpace(key))
		}
	}
	for _, key := range []string{"Connection", "Proxy-Connection", "Proxy-Authorization", "Proxy-Authenticate", "Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(key)
	}
}

func closeBrowserWrite(c net.Conn) {
	if half, ok := c.(interface{ CloseWrite() error }); ok {
		half.CloseWrite()
	} else {
		c.Close()
	}
}
func (c *bufferedBrowserConn) CloseWrite() error {
	if half, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return c.Conn.Close()
}
