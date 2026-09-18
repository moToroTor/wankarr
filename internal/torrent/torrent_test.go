package torrent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stub mimics Transmission: 409 + session id on first contact, then success.
func stub(t *testing.T, seen *[]string) *httptest.Server {
	t.Helper()
	var calls int
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		*seen = append(*seen, r.Header.Get("X-Transmission-Session-Id")+":"+req.Method)
		calls++
		if calls == 1 {
			w.Header().Set("X-Transmission-Session-Id", "sess-1")
			w.WriteHeader(http.StatusConflict)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result":    "success",
			"arguments": map[string]any{"torrent-added": map[string]any{"name": "Scene 8K", "id": 7}},
		})
	}))
}

func TestAddURLHandshake(t *testing.T) {
	var seen []string
	srv := stub(t, &seen)
	t.Cleanup(srv.Close)
	name, err := NewTransmission(srv.URL, "", "").AddURL("https://example.invalid/dl?id=1")
	if err != nil {
		t.Fatal(err)
	}
	if name != "Scene 8K" {
		t.Errorf("name = %q", name)
	}
	if len(seen) != 2 || seen[1] != "sess-1:torrent-add" {
		t.Errorf("session handshake not retried with id: %v", seen)
	}
}

func TestAddURLFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	if _, err := NewTransmission(srv.URL, "", "").AddURL("https://example.invalid/dl"); err == nil {
		t.Error("expected error on 403, got nil")
	}
}

func TestDuplicateReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result":    "success",
			"arguments": map[string]any{"torrent-duplicate": map[string]any{"name": "Old", "id": 3}},
		})
	}))
	t.Cleanup(srv.Close)
	name, err := NewTransmission(srv.URL, "", "").AddURL("https://example.invalid/dl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(name, "already queued") {
		t.Errorf("name = %q, want duplicate note", name)
	}
}

func TestAddURLSendsDownloadDir(t *testing.T) {
	var gotArgs map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		gotArgs = req.Arguments
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result":    "success",
			"arguments": map[string]any{"torrent-added": map[string]any{"name": "Scene", "id": 1}},
		})
	}))
	t.Cleanup(srv.Close)
	if _, err := NewTransmissionWithDir(srv.URL, "", "", "/mnt/media/vr").AddURL("https://example.invalid/dl"); err != nil {
		t.Fatal(err)
	}
	if gotArgs["download-dir"] != "/mnt/media/vr" {
		t.Errorf("download-dir = %v, want /mnt/media/vr", gotArgs["download-dir"])
	}
}

func TestAddURLOmitsDownloadDirWhenEmpty(t *testing.T) {
	var gotArgs map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		gotArgs = req.Arguments
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result":    "success",
			"arguments": map[string]any{"torrent-added": map[string]any{"name": "Scene", "id": 1}},
		})
	}))
	t.Cleanup(srv.Close)
	if _, err := NewTransmission(srv.URL, "", "").AddURL("https://example.invalid/dl"); err != nil {
		t.Fatal(err)
	}
	if _, ok := gotArgs["download-dir"]; ok {
		t.Errorf("download-dir = %v, want omitted for empty config", gotArgs["download-dir"])
	}
}
