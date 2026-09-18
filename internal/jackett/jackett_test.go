package jackett

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wankarr/internal/emp"
)

// Scrubbed Torznab fixture: same element/attr shape Jackett emits,
// fake host, no credentials.
const fixtureTorznab = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="1.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
<channel>
<item>
<title>VRCosplayX - The Legend of Vox Machina: Keyleth A XXX Parody - Gracey Snow (2026.09.17) (Oculus 8K)</title>
<guid>https://example.invalid/torrents.php?id=1154508</guid>
<link>https://example.invalid/torrents.php?id=1154508</link>
<pubDate>Thu, 18 Sep 2026 00:56:27 +0000</pubDate>
<size>23527860819</size>
<description>Verified: vrcosplayx.com gracey.snow virtual.reality 4096p 8k.vr</description>
<enclosure url="https://example.invalid/dl?id=1155042" length="150592" type="application/x-bittorrent" />
<torznab:attr name="category" value="Parody" />
<torznab:attr name="seeders" value="12" />
<torznab:attr name="downloadvolumefactor" value="0" />
<torznab:attr name="coverurl" value="https://example.invalid/poster.jpg" />
<torznab:attr name="infohash" value="abc" />
</item>
<item>
<title>Unrelated upload without usable link</title>
<guid>https://example.invalid/nope</guid>
<link>https://example.invalid/nope</link>
<pubDate>Thu, 18 Sep 2026 00:50:00 +0000</pubDate>
</item>
</channel>
</rss>`

func TestParseTorznab(t *testing.T) {
	items, err := ParseTorznab([]byte(fixtureTorznab), emp.GroupIDFromURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("parsed %d items, want 1 (linkless item skipped)", len(items))
	}
	it := items[0]
	if it.GroupID != "1154508" || it.SizeBytes != 23527860819 {
		t.Errorf("item = %+v", it)
	}
	for _, want := range []string{"vrcosplayx.com", "gracey.snow", "virtual.reality", "4096p"} {
		found := false
		for _, tag := range it.Tags {
			if tag == want {
				found = true
			}
		}
		if !found {
			t.Errorf("tags = %v, missing %q (parsed from description)", it.Tags, want)
		}
	}
	if it.Seeders != 12 {
		t.Errorf("seeders = %d, want 12", it.Seeders)
	}
	if !it.Freeleech {
		t.Error("freeleech = false, want true (downloadvolumefactor 0)")
	}
	if it.CoverURL != "https://example.invalid/poster.jpg" {
		t.Errorf("cover = %q", it.CoverURL)
	}
	if it.Source != "jackett" {
		t.Errorf("source = %q", it.Source)
	}
	if !strings.Contains(it.EnclosureURL, "example.invalid/dl") {
		t.Errorf("enclosure lost: %q", it.EnclosureURL)
	}
}

func TestSearchQueriesTorznabEndpoint(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(fixtureTorznab))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(srv.URL, "test-key", "/api/v2.0/indexers/empornium/results/torznab/")
	items, err := c.Search("Keyleth Gracey Snow", emp.GroupIDFromURL)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v2.0/indexers/empornium/results/torznab/" {
		t.Errorf("path = %q", gotPath)
	}
	for _, want := range []string{"t=search", "q=Keyleth", "apikey=test-key"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
}

func TestSearchServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "k", "/torznab/")
	if _, err := c.Search("x", emp.GroupIDFromURL); err == nil {
		t.Error("expected error on 500, got nil")
	}
}
