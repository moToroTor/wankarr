package doctor

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wankarr/internal/config"
)

func TestRunAllOK(t *testing.T) {
	xbvr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/scene/filters" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(xbvr.Close)
	jackett := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(jackett.Close)
	transmission := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Transmission-Session-Id", "s")
		w.WriteHeader(http.StatusConflict)
	}))
	t.Cleanup(transmission.Close)
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(feed.Close)

	cfg := &config.Config{
		XBVRURL:         xbvr.URL,
		RSSFeeds:        []string{feed.URL + "/feed?secret=abc"},
		JackettURL:      jackett.URL,
		JackettAPIKey:   "k",
		TransmissionURL: transmission.URL,
	}
	if code := Run(cfg); code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
}

func TestTransmissionDetailRedactsSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	r := checkTransmission(&http.Client{}, srv.URL, "admin", "s3cr!t")
	if strings.Contains(r.detail, "s3cr!t") {
		t.Errorf("secret leaked: %+v", r)
	}
	if !strings.Contains(r.detail, "passlen 6") || !strings.Contains(r.detail, `user "admin"`) {
		t.Errorf("diagnostic missing: %+v", r)
	}
}

func TestRunMissingConfigFails(t *testing.T) {
	cfg := &config.Config{}
	if code := Run(cfg); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
}

func TestTransmissionSendsBasicAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("X-Transmission-Session-Id", "s")
		w.WriteHeader(http.StatusConflict)
	}))
	t.Cleanup(srv.Close)
	r := checkTransmission(&http.Client{}, srv.URL, "admin", "s3cr!t")
	if !r.ok {
		t.Fatalf("check = %+v, want ok with auth", r)
	}
	if gotAuth == "" {
		t.Error("Authorization header missing: doctor would FAIL against auth-required RPC")
	}
}

func TestFeedURLRedacted(t *testing.T) {
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(feed.Close)

	cfg := &config.Config{RSSFeeds: []string{feed.URL + "/feed?authkey=SECRET&pass=SECRET"}}
	_ = cfg
	// checkFeed is unexported; exercise via host-only assertion on Run output
	// by capturing stdout is complex — instead assert the helper contract:
	r := checkFeed(&http.Client{}, feed.URL+"/feed?authkey=SECRET")
	if !strings.HasPrefix(r.name, "rss:") || strings.Contains(r.name, "SECRET") || strings.Contains(r.detail, "SECRET") {
		t.Errorf("feed check leaks secret: %+v", r)
	}
}
