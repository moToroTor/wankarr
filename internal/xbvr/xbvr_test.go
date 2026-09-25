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
				Files: []File{
					{VideoWidth: 3840, VideoHeight: 1920, Size: 35 << 30},
					{VideoWidth: 1920, VideoHeight: 960},
				},
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

// ListOwned collects matched file sizes for byte-equality matching;
// sizeless files are omitted, not zero-filled.
func TestListOwnedCollectsSizes(t *testing.T) {
	c, _ := stubServer(t)
	got, err := c.ListOwned()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Sizes) != 1 || got[0].Sizes[0] != 35<<30 {
		t.Fatalf("owned sizes = %+v", got)
	}
}

// appendStub serves the additive filenames endpoint over a stored list,
// merging posted names the way XBVR does. appendStatus forces the
// endpoint's status (0 means 200 with the merged list).
func appendStub(t *testing.T, stored []string, appendStatus int) (*Client, *[]string, *int) {
	t.Helper()
	var got []string
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/scene/filenames/9" {
			http.NotFound(w, r)
			return
		}
		posts++
		var req struct {
			Filenames []string `json:"filenames"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode append: %v", err)
		}
		got = req.Filenames
		if appendStatus != 0 {
			http.Error(w, "forced", appendStatus)
			return
		}
		have := map[string]bool{}
		for _, s := range stored {
			have[s] = true
		}
		for _, n := range req.Filenames {
			if !have[n] {
				stored = append(stored, n)
				have[n] = true
			}
		}
		_ = json.NewEncoder(w).Encode(stored)
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL), &got, &posts
}

// SeedFilenames posts names verbatim to the append endpoint; the server
// merges and returns the list.
func TestSeedFilenamesAppends(t *testing.T) {
	c, got, posts := appendStub(t, []string{"old.mp4"}, 0)
	added, err := c.SeedFilenames(9, []string{"old.mp4", "new_4k.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	if added != 2 {
		t.Errorf("added = %d, want 2 (names sent)", added)
	}
	if *posts != 1 {
		t.Fatalf("posts = %d, want 1", *posts)
	}
	if len(*got) != 2 || (*got)[0] != "old.mp4" || (*got)[1] != "new_4k.mp4" {
		t.Errorf("posted filenames = %q, want verbatim names", *got)
	}
}

// Empty input or an unknown scene id means no request at all.
func TestSeedFilenamesSkipsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL)
	if n, err := c.SeedFilenames(0, []string{"a.mp4"}); n != 0 || err != nil {
		t.Errorf("dbid 0: added = %d, err = %v; want 0, nil", n, err)
	}
	if n, err := c.SeedFilenames(9, nil); n != 0 || err != nil {
		t.Errorf("no names: added = %d, err = %v; want 0, nil", n, err)
	}
}

// Any non-200 append status (unknown scene, corrupt stored list, missing
// endpoint) surfaces as an error.
func TestSeedFilenamesSurfacesError(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		c, _, posts := appendStub(t, nil, status)
		if _, err := c.SeedFilenames(9, []string{"new.mp4"}); err == nil {
			t.Errorf("status %d: expected error, got nil", status)
		}
		if *posts != 1 {
			t.Errorf("status %d: posts = %d, want 1", status, *posts)
		}
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
