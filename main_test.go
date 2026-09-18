package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"wankarr/internal/config"
	"wankarr/internal/store"
	"wankarr/internal/xbvr"
)

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
