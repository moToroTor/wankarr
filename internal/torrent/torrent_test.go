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
	name, id, err := NewTransmission(srv.URL, "", "").AddURL("https://example.invalid/dl?id=1")
	if err != nil {
		t.Fatal(err)
	}
	if name != "Scene 8K" {
		t.Errorf("name = %q", name)
	}
	if id != 7 {
		t.Errorf("id = %d, want 7", id)
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
	if _, _, err := NewTransmission(srv.URL, "", "").AddURL("https://example.invalid/dl"); err == nil {
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
	name, id, err := NewTransmission(srv.URL, "", "").AddURL("https://example.invalid/dl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(name, "already queued") {
		t.Errorf("name = %q, want duplicate note", name)
	}
	if id != 3 {
		t.Errorf("id = %d, want duplicate id 3", id)
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
	if _, _, err := NewTransmissionWithDir(srv.URL, "", "", "/mnt/media/vr").AddURL("https://example.invalid/dl"); err != nil {
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
	if _, _, err := NewTransmission(srv.URL, "", "").AddURL("https://example.invalid/dl"); err != nil {
		t.Fatal(err)
	}
	if _, ok := gotArgs["download-dir"]; ok {
		t.Errorf("download-dir = %v, want omitted for empty config", gotArgs["download-dir"])
	}
}

func statusStub(t *testing.T, torrents []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if req.Method != "torrent-get" {
			t.Errorf("method = %q, want torrent-get", req.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result":    "success",
			"arguments": map[string]any{"torrents": torrents},
		})
	}))
}

func TestFileNamesReturnsBasenames(t *testing.T) {
	srv := statusStub(t, []map[string]any{
		{"id": 7, "files": []map[string]any{
			{"name": "Release Pack/video_4k.mp4", "length": 42},
			{"name": "Release Pack/cover.jpg", "length": 7},
			{"name": "loose.mkv", "length": 9},
		}},
	})
	t.Cleanup(srv.Close)
	got, err := NewTransmission(srv.URL, "", "").FileNames(7)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"video_4k.mp4", "cover.jpg", "loose.mkv"}
	if len(got) != len(want) {
		t.Fatalf("names = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFileNamesMissingTorrent(t *testing.T) {
	srv := statusStub(t, nil)
	t.Cleanup(srv.Close)
	if _, err := NewTransmission(srv.URL, "", "").FileNames(7); err != ErrTorrentNotFound {
		t.Errorf("err = %v, want ErrTorrentNotFound", err)
	}
}

func TestStatusOfDone(t *testing.T) {
	srv := statusStub(t, []map[string]any{
		{"id": 7, "name": "Scene 8K", "percentDone": 1, "status": 6, "isFinished": true},
	})
	t.Cleanup(srv.Close)
	st, err := NewTransmission(srv.URL, "", "").StatusOf(7)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Done() || st.Name != "Scene 8K" {
		t.Errorf("status = %+v, want done Scene 8K", st)
	}
}

func TestStatusOfDownloading(t *testing.T) {
	srv := statusStub(t, []map[string]any{
		{"id": 7, "name": "Scene 8K", "percentDone": 0.4, "status": 4, "isFinished": false},
	})
	t.Cleanup(srv.Close)
	st, err := NewTransmission(srv.URL, "", "").StatusOf(7)
	if err != nil {
		t.Fatal(err)
	}
	if st.Done() {
		t.Errorf("status = %+v, want not done", st)
	}
}

func TestStatusOfNotFound(t *testing.T) {
	srv := statusStub(t, nil)
	t.Cleanup(srv.Close)
	if _, err := NewTransmission(srv.URL, "", "").StatusOf(99); err != ErrTorrentNotFound {
		t.Errorf("err = %v, want ErrTorrentNotFound", err)
	}
}
