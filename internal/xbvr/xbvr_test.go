package xbvr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func stubServer(t *testing.T) (*Client, *[]map[string]any) {
	t.Helper()
	var seen []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/scene/list" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		seen = append(seen, req)
		offset := int(req["offset"].(float64))
		var scenes []Scene
		if offset == 0 {
			scenes = []Scene{{
				SceneID: "fuckpassvr-001", Title: "Rainy City Rendezvous",
				Site: "FuckPassVR", Wishlist: true,
				Files: []File{{VideoWidth: 3840, VideoHeight: 1920}},
			}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"scenes": scenes, "results": len(scenes)})
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL), &seen
}

func TestListWishlist(t *testing.T) {
	c, seen := stubServer(t)
	got, err := c.ListWishlist()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SceneID != "fuckpassvr-001" {
		t.Fatalf("wishlist = %+v", got)
	}
	if lists, _ := (*seen)[0]["lists"].([]any); len(lists) != 1 || lists[0] != "wishlist" {
		t.Errorf("request lists = %v, want [wishlist]", (*seen)[0]["lists"])
	}
}

func TestListOwnedPicksBestHeight(t *testing.T) {
	c, _ := stubServer(t)
	got, err := c.ListOwned()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].BestHeight != 1920 {
		t.Fatalf("owned = %+v", got)
	}
}

func TestServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	if _, err := NewClient(srv.URL).ListWishlist(); err == nil {
		t.Error("expected error on 500, got nil")
	}
}

func TestRescanQueuesTask(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
	}))
	t.Cleanup(srv.Close)
	if err := NewClient(srv.URL).Rescan(); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/task/rescan" {
		t.Errorf("request = %s %s, want GET /api/task/rescan", gotMethod, gotPath)
	}
}

func TestFindFilePrefersUnmatched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/files/list" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]File{
			{ID: 1, SceneID: 9, Filename: "Scene 8K.mp4"},
			{ID: 2, SceneID: 0, Filename: "Scene 8K (1).mp4"},
		})
	}))
	t.Cleanup(srv.Close)
	f, found, err := NewClient(srv.URL).FindFile("Scene 8K")
	if err != nil {
		t.Fatal(err)
	}
	if !found || f.ID != 2 {
		t.Errorf("file = %+v found=%v, want unmatched ID 2", f, found)
	}
}

func TestFindFileNone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]File{})
	}))
	t.Cleanup(srv.Close)
	if _, found, err := NewClient(srv.URL).FindFile("nope"); err != nil || found {
		t.Errorf("found=%v err=%v, want not found", found, err)
	}
}

func TestMatchFilePayload(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/files/match" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	t.Cleanup(srv.Close)
	if err := NewClient(srv.URL).MatchFile("fuckpassvr-001", 2); err != nil {
		t.Fatal(err)
	}
	if got["scene_id"] != "fuckpassvr-001" || got["file_id"] != float64(2) {
		t.Errorf("payload = %v, want scene_id + file_id", got)
	}
}
