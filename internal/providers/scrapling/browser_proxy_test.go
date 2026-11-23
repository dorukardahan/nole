package scrapling

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBrowserDialPinsDNSAndRejectsRebinding(t *testing.T) {
	lookups, dials := 0, 0
	d := browserDialer{
		lookup: func(context.Context, string) ([]net.IP, error) {
			lookups++
			if lookups > 1 {
				return []net.IP{net.ParseIP("127.0.0.1")}, nil
			}
			return []net.IP{net.ParseIP("8.8.8.8")}, nil
		},
		dial: func(_ context.Context, network, address string) (net.Conn, error) {
			dials++
			if address != "8.8.8.8:443" {
				t.Errorf("dial was not pinned: %q", address)
			}
			a, b := net.Pipe()
			b.Close()
			return a, nil
		},
	}
	c, err := d.connect(context.Background(), "tcp", "rebind.example:443")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if _, err = d.connect(context.Background(), "tcp", "rebind.example:443"); err == nil {
		t.Fatal("rebound private address accepted")
	}
	if lookups != 2 || dials != 1 {
		t.Fatalf("lookups=%d dials=%d", lookups, dials)
	}
	for _, ips := range [][]net.IP{nil, {net.ParseIP("8.8.8.8"), net.ParseIP("169.254.169.254")}, {net.ParseIP("::ffff:127.0.0.1")}} {
		d.lookup = func(context.Context, string) ([]net.IP, error) { return ips, nil }
		if _, err := d.connect(context.Background(), "tcp", "mixed.example:80"); err == nil {
			t.Fatal("invalid DNS answers accepted")
		}
	}
	if dials != 1 {
		t.Fatal("blocked DNS reached dialer")
	}
}

func TestBrowserProxyRejectsPrivateHTTPAndCONNECT(t *testing.T) {
	calls := 0
	d := browserDialer{lookup: func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("127.0.0.1")}, nil }, dial: func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, fmt.Errorf("must not dial")
	}}
	raw, stop, err := listenBrowserProxy(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	proxy, _ := url.Parse(raw)
	password, _ := proxy.User.Password()
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("nole:"+password))
	for _, target := range []string{"127.0.0.1", "169.254.169.254", "10.0.0.1", "[::1]", "[::ffff:127.0.0.1]", "localhost", "rebind.example"} {
		for _, method := range []string{"GET", "CONNECT"} {
			c, err := net.Dial("tcp", proxy.Host)
			if err != nil {
				t.Fatal(err)
			}
			request, err := http.NewRequest(method, "http://"+target+"/", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Proxy-Authorization", auth)
			request.Close = true
			if method == "CONNECT" {
				request.URL = &url.URL{Host: target + ":443"}
				request.Host = target + ":443"
				err = request.Write(c)
			} else {
				err = request.WriteProxy(c)
			}
			if err != nil {
				c.Close()
				t.Fatal(err)
			}

			r, err := http.ReadResponse(bufio.NewReader(c), nil)
			if err != nil {
				c.Close()
				t.Fatal(err)
			}
			if r.StatusCode != 403 {
				t.Errorf("%s %s status=%d", method, target, r.StatusCode)
			}
			r.Body.Close()
			c.Close()
		}
	}
	if calls != 0 {
		t.Fatal("private connection escaped validation")
	}
}

func TestBrowserUpstreamReceivesPinnedIPAndPrivateAuth(t *testing.T) {
	observed := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" {
			t.Error("expected CONNECT")
		}
		observed <- r.Host
		if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:synthetic")) {
			t.Error("missing upstream authentication")
		}
		c, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer c.Close()
		rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\nhello")
		rw.Flush()
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	u.User = url.UserPassword("user", "synthetic")
	d := browserDialer{upstream: u, lookup: func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("8.8.8.8")}, nil }, dial: (&net.Dialer{}).DialContext}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, err := d.connect(ctx, "tcp", "public.example:443")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	data, err := io.ReadAll(c)
	if err != nil || string(data) != "hello" {
		t.Fatalf("buffered tunnel read failed: %v", err)
	}
	if got := <-observed; got != "8.8.8.8:443" {
		t.Fatalf("upstream resolved hostname: %s", got)
	}
}

func TestBrowserProxyCredentialsNeverReachTarget(t *testing.T) {
	seen := make(chan http.Header, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen <- r.Header; w.Write([]byte("article")) }))
	defer target.Close()
	d := browserDialer{lookup: func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("8.8.8.8")}, nil }, dial: func(ctx context.Context, n, a string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, n, strings.TrimPrefix(target.URL, "http://"))
	}}
	raw, stop, err := listenBrowserProxy(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	u, _ := url.Parse(raw)
	tr := &http.Transport{Proxy: http.ProxyURL(u)}
	defer tr.CloseIdleConnections()
	client := http.Client{Transport: tr, Timeout: time.Second}
	r, err := client.Get("http://public.example/")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if h := <-seen; h.Get("Proxy-Authorization") != "" {
		t.Fatal("local credentials escaped")
	}
}

func TestBrowserProxyAuthAndLocalReadiness(t *testing.T) {
	d := browserDialer{lookup: func(context.Context, string) ([]net.IP, error) {
		t.Error("readiness attempted DNS")
		return nil, errBrowserNetwork
	}, dial: func(context.Context, string, string) (net.Conn, error) {
		t.Error("readiness attempted dial")
		return nil, errBrowserNetwork
	}}
	raw, stop, err := listenBrowserProxy(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	u, _ := url.Parse(raw)
	for _, valid := range []bool{false, true} {
		endpoint := *u
		if !valid {
			endpoint.User = nil
		}
		tr := &http.Transport{Proxy: http.ProxyURL(&endpoint)}
		client := http.Client{Transport: tr, Timeout: time.Second}
		resp, err := client.Get("http://nole-readiness.invalid/")
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		tr.CloseIdleConnections()
		if valid {
			if resp.StatusCode != 200 || string(data) != "nole browser proxy ready" {
				t.Fatal("readiness response missing")
			}
		} else if resp.StatusCode != 407 {
			t.Fatal("proxy accepted unauthenticated client")
		}
	}
}

func TestBrowserTunnelPreservesResponseAfterClientHalfClose(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		c, err := listener.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		io.ReadAll(c)
		c.Write([]byte("tail"))
	}()
	d := browserDialer{lookup: func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("8.8.8.8")}, nil }, dial: func(ctx context.Context, n, a string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, n, listener.Addr().String())
	}}
	raw, stop, err := listenBrowserProxy(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	u, _ := url.Parse(raw)
	c, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	password, _ := u.User.Password()
	req := &http.Request{Method: "CONNECT", URL: &url.URL{Host: "public.example:443"}, Host: "public.example:443", Header: make(http.Header)}
	req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("nole:"+password)))
	if err := req.Write(c); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(c)
	resp, err := http.ReadResponse(reader, req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatal("CONNECT failed")
	}
	c.Write([]byte("request"))
	c.(*net.TCPConn).CloseWrite()
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != "tail" {
		t.Fatalf("half-close truncated response: %q %v", body, err)
	}
	<-finished
}

func TestBrowserProxyStopClosesHijackedTunnel(t *testing.T) {
	upstreamClosed := make(chan struct{})
	d := browserDialer{lookup: func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("8.8.8.8")}, nil }, dial: func(context.Context, string, string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() { io.Copy(io.Discard, b); b.Close(); close(upstreamClosed) }()
		return a, nil
	}}
	raw, stop, err := listenBrowserProxy(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	u, _ := url.Parse(raw)
	c, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	password, _ := u.User.Password()
	req := &http.Request{Method: "CONNECT", URL: &url.URL{Host: "public.example:443"}, Host: "public.example:443", Header: make(http.Header)}
	req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("nole:"+password)))
	if err := req.Write(c); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(c)
	response, err := http.ReadResponse(reader, req)
	if err != nil || response.StatusCode != 200 {
		t.Fatal("CONNECT failed")
	}
	stop()
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("tunnel remained open")
	}
	select {
	case <-upstreamClosed:
	case <-time.After(time.Second):
		t.Fatal("proxy shutdown left upstream connection open")
	}
}
