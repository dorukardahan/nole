package selfupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// tagServer returns an httptest server that responds with the given tag_name,
// and fails the test if the request carries an Authorization header (the check
// must stay anonymous).
func tagServer(t *testing.T, tag string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("selfupdate must not send an Authorization header; got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `","name":"rel"}`))
	}))
}

func TestCheckLatestStaleAgainstNewerRelease(t *testing.T) {
	srv := tagServer(t, "v9.9.9")
	defer srv.Close()
	got := checkLatest(context.Background(), "0.9.0", srv.URL, srv.Client())
	if !got.Checked || !got.Stale || got.Latest != "v9.9.9" {
		t.Fatalf("want checked+stale+latest=v9.9.9, got %+v", got)
	}
}

func TestCheckLatestUpToDate(t *testing.T) {
	srv := tagServer(t, "v0.9.0")
	defer srv.Close()
	got := checkLatest(context.Background(), "0.9.0", srv.URL, srv.Client())
	if !got.Checked || got.Stale {
		t.Fatalf("want checked + not stale, got %+v", got)
	}
}

func TestCheckLatestOfflineIsSilentAndFailSoft(t *testing.T) {
	srv := tagServer(t, "v9.9.9")
	url := srv.URL
	srv.Close() // now unreachable
	got := checkLatest(context.Background(), "0.9.0", url, &http.Client{Timeout: time.Second})
	if got.Checked || got.Stale {
		t.Fatalf("offline must be Checked:false, Stale:false (silent), got %+v", got)
	}
}

func TestCheckLatestNon200IsFailSoft(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	got := checkLatest(context.Background(), "0.9.0", srv.URL, srv.Client())
	if got.Checked {
		t.Fatalf("non-200 must be fail-soft (Checked:false), got %+v", got)
	}
}

func TestCheckLatestEmptyTagIsFailSoft(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":""}`))
	}))
	defer srv.Close()
	got := checkLatest(context.Background(), "0.9.0", srv.URL, srv.Client())
	if got.Checked {
		t.Fatalf("empty tag must be fail-soft, got %+v", got)
	}
}

func TestCheckLatestDevBuildDoesNotWarn(t *testing.T) {
	srv := tagServer(t, "v0.9.0")
	defer srv.Close()
	for _, cur := range []string{"dev", "dev-build-check", "unknown", ""} {
		got := checkLatest(context.Background(), cur, srv.URL, srv.Client())
		if got.Stale {
			t.Fatalf("non-release current %q must not be flagged stale, got %+v", cur, got)
		}
		if got.Comparable {
			t.Fatalf("non-release current %q must be reported NOT comparable, got %+v", cur, got)
		}
	}
}

func TestCheckLatestReleaseBuildIsComparable(t *testing.T) {
	srv := tagServer(t, "v0.9.0")
	defer srv.Close()
	got := checkLatest(context.Background(), "0.9.0", srv.URL, srv.Client())
	if !got.Checked || !got.Comparable {
		t.Fatalf("a release-shaped current must be comparable, got %+v", got)
	}
}

func TestCheckLatestRespectsContextTimeoutWithoutHang(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client cancels (ctx deadline) so the server goroutine
		// doesn't leak past the client's cancellation.
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := checkLatest(ctx, "0.9.0", srv.URL, srv.Client())
	if got.Checked {
		t.Fatalf("a timed-out request must be fail-soft, got %+v", got)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("check hung for %v; should return near the context deadline", elapsed)
	}
}

func TestCheckLatestMalformedBaseURLIsFailSoft(t *testing.T) {
	for _, base := range []string{"ht!tp://bad", "://nope", "http://127.0.0.1:0"} {
		got := checkLatest(context.Background(), "0.9.0", base, &http.Client{Timeout: time.Second})
		if got.Checked {
			t.Fatalf("malformed base %q must be fail-soft, got %+v", base, got)
		}
	}
}

func TestIsOlderTable(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"0.8.0", "0.9.0", true},
		{"v0.8.0", "v0.9.0", true},
		{"0.9.0", "0.9.0", false},
		{"v0.9.0", "0.9.0", false}, // 'v' prefix on one side only
		{"0.9.0", "0.9.1", true},
		{"0.10.0", "0.9.0", false}, // numeric, not lexical: 10 > 9
		{"0.9.0", "0.10.0", true},
		{"1.0.0", "0.9.9", false},
		{"dev", "0.9.0", false}, // non-release current
		{"dev-build-check", "0.9.0", false},
		{"0.9.0", "garbage", false},   // non-release latest
		{"1.2", "1.2.0", false},       // unequal segment count → not release-shaped
		{"1.2.3-rc1", "1.2.3", false}, // pre-release base equals → not older
		{"1.2.3-rc1", "1.3.0", true},  // pre-release base is behind 1.3.0
	}
	for _, c := range cases {
		if got := isOlder(c.current, c.latest); got != c.want {
			t.Errorf("isOlder(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}
