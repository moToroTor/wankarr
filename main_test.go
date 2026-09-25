package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wankarr/internal/config"
	"wankarr/internal/emp"
	"wankarr/internal/store"
	"wankarr/internal/xbvr"
)

// The library snapshot is cached: the second view in a row makes no
// new XBVR request, and a failed refresh past TTL keeps the stale list.
func TestGetOwnedCachesAndKeepsStale(t *testing.T) {
	ownedCache.at, ownedCache.list = time.Time{}, nil
	var calls int
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"scenes": []map[string]any{{
				"id": 3, "scene_id": "vp-1", "title": "SfizyDyd", "site": "Virtual Papi",
				"file": []map[string]any{{"video_height": 720}},
			}},
		})
	}))
	t.Cleanup(srv.Close)
	xc := xbvr.NewClient(srv.URL)
	got := getOwned(xc)
	if len(got) != 1 || got[0].BestHeight != 720 {
		t.Fatalf("snapshot = %+v, want one 720p scene", got)
	}
	if again := getOwned(xc); len(again) != 1 {
		t.Fatalf("cached = %+v, want 1", again)
	}
	if calls != 1 {
		t.Fatalf("xbvr calls = %d, want 1 (second served from cache)", calls)
	}
	ownedCache.at = time.Time{}
	fail = true
	if stale := getOwned(xc); len(stale) != 1 {
		t.Fatalf("stale = %+v, want the cached scene", stale)
	}
	if calls != 2 {
		t.Fatalf("xbvr calls = %d, want 2 (one retry, then stale)", calls)
	}
}

// Groups already matched in the XBVR library carry their best local
// height; unowned groups carry none.
func TestBuildGroupViewsMarksOwned(t *testing.T) {
	items := []store.Item{
		{GroupID: "a", Title: "[Virtual Papi] SfizyDyd (Next Door Peep) 2K", Tags: []string{"virtual.reality"}, SizeBytes: 4 << 30},
		{GroupID: "b", Title: "Some Other Scene 4K", Tags: []string{"virtual.reality"}, SizeBytes: 4 << 30},
	}
	owned := []xbvr.OwnedScene{
		{SceneID: "vp-1", Title: "SfizyDyd (Next Door Peep)", Site: "Virtual Papi", BestHeight: 720},
	}
	views := buildGroupViews(emp.Profile{}, &config.Config{}, nil, owned, items, false)
	if len(views) != 2 {
		t.Fatalf("views = %d, want 2", len(views))
	}
	for _, v := range views {
		switch v.Key {
		case "[virtual papi] sfizydyd (next door peep)":
			if v.OwnedHeight != 720 {
				t.Errorf("owned group height = %d, want 720", v.OwnedHeight)
			}
		case "some other scene":
			if v.OwnedHeight != 0 {
				t.Errorf("unowned group height = %d, want 0", v.OwnedHeight)
			}
		default:
			t.Errorf("unexpected group key %q", v.Key)
		}
	}
}

func TestFilterQuery(t *testing.T) {
	items := []store.Item{
		{GroupID: "1", Title: "FuckPassVR - Rainy City Rendezvous - Mia James (Oculus 8K)", Tags: []string{"mia.james", "virtual.reality"}},
		{GroupID: "2", Title: "VRCosplayX - Keyleth Parody - Gracey Snow (Oculus 8K)", Tags: []string{"gracey.snow"}},
	}
	if got := filterQuery(items, ""); len(got) != 2 {
		t.Errorf("empty q keeps all, got %d", len(got))
	}
	got := filterQuery(items, "mia james")
	if len(got) != 1 || got[0].GroupID != "1" {
		t.Errorf("performer query = %+v", got)
	}
	got = filterQuery(items, "LAUREN phillips")
	if len(got) != 0 {
		t.Errorf("non-matching query = %+v, want none", got)
	}
	got = filterQuery(items, "gracey 8K")
	if len(got) != 1 {
		t.Errorf("title+token query = %+v, want the Gracey group", got)
	}
	got = filterQuery(items, "virtual.reality")
	if len(got) != 1 || got[0].GroupID != "1" {
		t.Errorf("tag query = %+v, want tag match", got)
	}
}

// The reported bug: a wishlist entry added AFTER the Emp rows were stored
// never paired. matchAll must pair old rows with the current wishlist.
func TestMatchAllPairsOldItems(t *testing.T) {
	xbvrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": 1,
			"scenes": []map[string]any{{
				"scene_id": "fp-1", "title": "Rainy City Rendezvous",
				"site": "FuckPassVR",
			}},
		})
	}))
	t.Cleanup(xbvrSrv.Close)

	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	stored, err := db.UpsertItem(store.Item{
		GroupID: "1154515",
		Title:   "FuckPassVR - Rainy City Rendezvous - Mia James (2026.08.28) (Oculus 8K)",
		Tags:    []string{"mia.james", "3840p", "virtual.reality"},
		PubDate: time.Now().UTC(), FetchedAt: time.Now().UTC(),
	})
	if err != nil || !stored {
		t.Fatalf("upsert = %v, %v", stored, err)
	}

	matchAll(db, xbvr.NewClient(xbvrSrv.URL))

	ms, err := db.ListMatches(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].SceneID != "fp-1" {
		t.Fatalf("matches = %+v, want the fp-1 pairing", ms)
	}
}

func TestServeWishlist(t *testing.T) {
	xbvrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": 1,
			"scenes": []map[string]any{{
				"scene_id": "fp-1", "title": "Rainy City Rendezvous",
				"site": "FuckPassVR", "cover_url": "/cache/cover.jpg",
				"cast": []map[string]any{{"name": "Mia James"}},
			}},
		})
	}))
	t.Cleanup(xbvrSrv.Close)

	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.UpsertItem(store.Item{
		GroupID: "1154515",
		Title:   "FuckPassVR - Rainy City Rendezvous - Mia James (2026.08.28) (Oculus 8K)",
		Tags:    []string{"mia.james", "3840p", "virtual.reality"},
		PubDate: time.Now().UTC(), FetchedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{XBVRURL: "http://xbvr:9999"}
	rec := httptest.NewRecorder()
	serveWishlist(db, cfg, xbvr.NewClient(xbvrSrv.URL), rec, httptest.NewRequest(http.MethodGet, "/api/wishlist", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["title"] != "Rainy City Rendezvous" {
		t.Fatalf("wishlist = %v", got)
	}
	if got[0]["cover"] != "http://xbvr:9999/cache/cover.jpg" {
		t.Errorf("cover = %v, want absolutized XBVR URL", got[0]["cover"])
	}
	perfs, _ := got[0]["performers"].([]any)
	if len(perfs) != 1 || perfs[0] != "Mia James" {
		t.Errorf("performers = %v, want [Mia James]", got[0]["performers"])
	}
	groups, _ := got[0]["groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("groups = %v, want the known Emp group inline", got[0]["groups"])
	}
}

func sendTestServers(t *testing.T, wishlistScenes []map[string]any) (transURL, xbvrURL string) {
	t.Helper()
	trans := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result":    "success",
			"arguments": map[string]any{"torrent-added": map[string]any{"name": "Scene 8K", "id": 7}},
		})
	}))
	t.Cleanup(trans.Close)
	xbvrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": len(wishlistScenes),
			"scenes":  wishlistScenes,
		})
	}))
	t.Cleanup(xbvrSrv.Close)
	return trans.URL, xbvrSrv.URL
}

func seedSendItem(t *testing.T, db *store.DB) {
	t.Helper()
	_, err := db.UpsertItem(store.Item{
		GroupID:      "1154515",
		Title:        "FuckPassVR - Rainy City Rendezvous - Mia James (2026.08.28) (Oculus 8K)",
		Tags:         []string{"mia.james", "3840p", "virtual.reality"},
		EnclosureURL: "https://example.invalid/torrents.php?action=download&id=1",
		PubDate:      time.Now().UTC(), FetchedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func postSend(t *testing.T, cfg *config.Config, db *store.DB) (int, map[string]any) {
	t.Helper()
	body := `{"group_id":"1154515"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/send", strings.NewReader(body))
	serveSend(cfg, db, rec, req)
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return rec.Code, got
}

// A wishlist grab must leave a pending row so the completion poller can
// link the finished file into its XBVR scene.
func TestServeSendTracksWishlistPending(t *testing.T) {
	transURL, xbvrURL := sendTestServers(t, []map[string]any{{
		"scene_id": "fp-1", "title": "Rainy City Rendezvous", "site": "FuckPassVR",
	}})
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	seedSendItem(t, db)

	cfg := &config.Config{TransmissionURL: transURL, XBVRURL: xbvrURL}
	code, got := postSend(t, cfg, db)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body %v", code, got)
	}
	if got["wishlist_scene"] != "fp-1" {
		t.Errorf("wishlist_scene = %v, want fp-1", got["wishlist_scene"])
	}
	pend, err := db.ListActivePending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) != 1 || pend[0].SceneID != "fp-1" || pend[0].TransmissionID != 7 {
		t.Fatalf("pending = %+v, want one fp-1 row with torrent 7", pend)
	}
}

// A wishlist send registers the grab's inner video filenames on the
// scene, so the next XBVR scan auto-matches them.
func TestServeSendSeedsFilenames(t *testing.T) {
	trans := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method    string         `json:"method"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if req.Method == "torrent-get" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": "success",
				"arguments": map[string]any{"torrents": []map[string]any{{
					"id": 7, "files": []map[string]any{
						{"name": "Rainy City/rainy_city_8k.mp4", "length": 99},
						{"name": "Rainy City/cover.jpg", "length": 1},
					},
				}}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result":    "success",
			"arguments": map[string]any{"torrent-added": map[string]any{"name": "Scene 8K", "id": 7}},
		})
	}))
	t.Cleanup(trans.Close)
	var appendBody struct {
		Filenames []string `json:"filenames"`
	}
	xbvrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/scene/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": 1,
				"scenes": []map[string]any{{
					"id": 9, "scene_id": "fp-1", "title": "Rainy City Rendezvous",
					"site": "FuckPassVR", "filenames_arr": `["old.mp4"]`,
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/scene/filenames/9":
			if err := json.NewDecoder(r.Body).Decode(&appendBody); err != nil {
				t.Errorf("decode append: %v", err)
			}
			_ = json.NewEncoder(w).Encode([]string{"old.mp4", "rainy_city_8k.mp4"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(xbvrSrv.Close)

	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	seedSendItem(t, db)

	cfg := &config.Config{TransmissionURL: trans.URL, XBVRURL: xbvrSrv.URL}
	code, got := postSend(t, cfg, db)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body %v", code, got)
	}
	if got["seeded_filenames"] != float64(1) {
		t.Errorf("seeded_filenames = %v, want 1", got["seeded_filenames"])
	}
	if len(appendBody.Filenames) != 1 || appendBody.Filenames[0] != "rainy_city_8k.mp4" {
		t.Errorf("appended filenames = %q, want just the fresh inner name", appendBody.Filenames)
	}
}

// A non-wishlist send stays fire-and-forget: no pending row.
func TestServeSendNoPendingWithoutWishlistMatch(t *testing.T) {
	transURL, xbvrURL := sendTestServers(t, nil)
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	seedSendItem(t, db)

	cfg := &config.Config{TransmissionURL: transURL, XBVRURL: xbvrURL}
	code, got := postSend(t, cfg, db)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body %v", code, got)
	}
	if _, ok := got["wishlist_scene"]; ok {
		t.Errorf("wishlist_scene present = %v, want absent", got)
	}
	pend, err := db.ListActivePending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) != 0 {
		t.Fatalf("pending = %+v, want none", pend)
	}
}
