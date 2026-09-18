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
