package xbvr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func stubServer(t *testing.T) (*Client, *[]map[string]any) {
	t.Helper()
	var mu sync.Mutex
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
		mu.Lock()
		seen = append(seen, req)
		mu.Unlock()
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

// pagedStub serves total scenes in limit-sized chunks by offset,
// delaying the second page so completion order differs from offset
// order.
func pagedStub(t *testing.T, total int, fail bool) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		offset := int(req["offset"].(float64))
		limit := int(req["limit"].(float64))
		if offset == ownedPageSize {
			time.Sleep(100 * time.Millisecond)
		}
		var scenes []Scene
		for i := offset; i < offset+limit && i < total; i++ {
			scenes = append(scenes, Scene{
				SceneID:  fmt.Sprintf("scene-%04d", i),
				Title:    fmt.Sprintf("Scene %04d Here", i),
				Site:     "Site",
				CoverURL: fmt.Sprintf("https://xbvr/cover/%04d.jpg", i),
				Files:    []File{{VideoHeight: 1080}},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"scenes": scenes})
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL)
}

// Concurrent pages merge in offset order (tie-breaking in the library
// index depends on it), complete, and carry covers — even though the
// second page finishes last here.
func TestListOwnedMergesPagesInOrder(t *testing.T) {
	got, err := pagedStub(t, 450, false).ListOwned()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 450 {
		t.Fatalf("scenes = %d, want 450", len(got))
	}
	for i, s := range got {
		if want := fmt.Sprintf("scene-%04d", i); s.SceneID != want {
			t.Fatalf("scene %d = %q, want %q (order broke)", i, s.SceneID, want)
		}
	}
	if got[0].CoverURL != "https://xbvr/cover/0000.jpg" {
		t.Errorf("cover = %q, want the snapshot artwork", got[0].CoverURL)
	}
}

// Any page error fails the snapshot; the caller serves stale instead.
func TestListOwnedSurfacesError(t *testing.T) {
	if _, err := pagedStub(t, 450, true).ListOwned(); err == nil {
		t.Error("expected an error, got nil")
	}
}

// wishlistStub speaks the scrape/wishlist flow: scene search, one
// scene read, candidate search, trust pick, and the wishlist toggle.
// Request shapes are asserted; t.Errorf only (handlers run off-test).
func wishlistStub(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enc := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch r.URL.Path {
		case "/api/scene/search":
			if q := r.URL.Query().Get("q"); q != "francii luscious" {
				t.Errorf("search q = %q, want the title query", q)
			}
			enc(map[string]any{"scenes": []Scene{{ID: 7, SceneID: "slr-1", Title: "First Titty Drop", Site: "SexLikeReal"}}})
		case "/api/scene/7":
			enc(Scene{ID: 7, SceneID: "slr-1", Title: "First Titty Drop", Wishlist: false})
		case "/api/task/scrape-search":
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode scrape-search: %v", err)
			}
			if req["q"] != "sexlikereal first titty drop" {
				t.Errorf("scrape-search q = %v", req["q"])
			}
			enc(map[string]any{"status": "OK", "candidates": []ScrapeCandidate{
				{ScraperID: "slr", ScraperName: "SexLikeReal", Domain: "sexlikereal.com", URL: "https://sexlikereal.com/s/1", Title: "First Titty Drop", Reason: "exact", Preferred: true},
			}})
		case "/api/task/scrape-pick":
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode scrape-pick: %v", err)
			}
			title, _ := req["title"].(string)
			if (title != "First Titty Drop" && title != "Unknown Scene Here") || req["wishlist"] != true {
				t.Errorf("scrape-pick body = %v, want title/wishlist", req)
			}
			if _, ok := req["performers"].([]any); !ok {
				t.Errorf("scrape-pick performers = %v (%T), want an array", req["performers"], req["performers"])
			}
			if title == "Unknown Scene Here" {
				enc(map[string]any{"status": "OK", "scene_id": 0, "wishlisted": false})
				return
			}
			enc(map[string]any{"status": "OK", "scene_id": 5, "wishlisted": true,
				"candidate": map[string]any{"scraper_id": "slr", "url": "https://sexlikereal.com/s/1"}})
		case "/api/scene/toggle":
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode toggle: %v", err)
			}
			if req["scene_id"] != "slr-1" || req["list"] != "wishlist" {
				t.Errorf("toggle body = %v, want scene_id+list", req)
			}
			enc(map[string]any{"status": "OK"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL)
}

func TestSearchScenesHits(t *testing.T) {
	got, err := wishlistStub(t).SearchScenes("francii luscious")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SceneID != "slr-1" {
		t.Fatalf("search = %+v, want the slr-1 scene", got)
	}
}

func TestGetSceneReadsFlags(t *testing.T) {
	s, err := wishlistStub(t).GetScene(7)
	if err != nil {
		t.Fatal(err)
	}
	if s.Wishlist || s.Title != "First Titty Drop" {
		t.Errorf("scene = %+v, want unwishlisted with title", s)
	}
}

func TestScrapeSearchCandidates(t *testing.T) {
	got, err := wishlistStub(t).ScrapeSearch("sexlikereal first titty drop")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Preferred || got[0].ScraperName != "SexLikeReal" {
		t.Fatalf("candidates = %+v, want the preferred slr hit", got)
	}
}

func TestScrapePickTrustsRanking(t *testing.T) {
	res, err := wishlistStub(t).ScrapePick("First Titty Drop", "SexLikeReal", nil, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.SceneID != 5 || !res.Wishlisted || res.Candidate.URL != "https://sexlikereal.com/s/1" {
		t.Errorf("pick = %+v, want scene 5 wishlisted with candidate echo", res)
	}
}

func TestScrapePickZeroMeansUnknown(t *testing.T) {
	res, err := wishlistStub(t).ScrapePick("Unknown Scene Here", "No Site", nil, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.SceneID != 0 || res.Wishlisted {
		t.Errorf("pick = %+v, want scene 0 not wishlisted", res)
	}
}

func TestToggleWishlistPayload(t *testing.T) {
	if err := wishlistStub(t).ToggleWishlist("slr-1"); err != nil {
		t.Fatal(err)
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
