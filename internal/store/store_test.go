package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestUpsertAndCount(t *testing.T) {
	db := openTemp(t)
	it := Item{
		GroupID:    "1154515",
		Title:      "FuckPassVR - Rainy City Rendezvous - Mia James (2026.08.28) (Oculus 8K, UHD)",
		Category:   "Big Tits",
		CategoryID: "8",
		PubDate:    time.Date(2026, 9, 18, 1, 32, 19, 0, time.UTC),
		Tags:       []string{"fuckpassvr.com", "mia.james", "4096p", "high.bitrate", "virtual.reality"},
		SizeBytes:  25422398398,
		Filename:   "FuckPassVR - Rainy City Rendezvous - Mia James (Oculus 8K, UHD).torrent",
		Infohash:   "abc123",
		DetailsURL: "https://example.invalid/torrents.php?id=1154515",
		Source:     "rss",
		FetchedAt:  time.Now().UTC(),
	}
	isNew, err := db.UpsertItem(it)
	if err != nil {
		t.Fatal(err)
	}
	if !isNew {
		t.Error("first upsert should report new=true")
	}
	isNew, err = db.UpsertItem(it)
	if err != nil {
		t.Fatal(err)
	}
	if isNew {
		t.Error("second upsert of same group should report new=false")
	}
	n, err := db.Count()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("Count = %d, want 1 (upsert must dedupe on group_id)", n)
	}
	got, err := db.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != it.Title {
		t.Fatalf("Recent = %+v", got)
	}
	if len(got[0].Tags) != 5 || got[0].SizeBytes != 25422398398 {
		t.Errorf("Recent item tags/size = %v/%d", got[0].Tags, got[0].SizeBytes)
	}
	if got[0].EnclosureURL != "" {
		t.Error("Recent must not return the credential-bearing enclosure URL")
	}
}

func TestSeedersFreeleechRoundTrip(t *testing.T) {
	db := openTemp(t)
	it := Item{GroupID: "1", Title: "T", PubDate: time.Now().UTC(), FetchedAt: time.Now().UTC(), Seeders: 12, Freeleech: true, CoverURL: "https://example.invalid/c.jpg"}
	if _, err := db.UpsertItem(it); err != nil {
		t.Fatal(err)
	}
	got, err := db.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Seeders != 12 || !got[0].Freeleech || got[0].CoverURL != "https://example.invalid/c.jpg" {
		t.Fatalf("round trip = %+v", got)
	}
	u, err := db.EnclosureURL("missing")
	if err == nil || u != "" {
		t.Error("EnclosureURL on missing group should error")
	}
}

func TestLegacyDBMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Schema as it existed before seeders/freeleech/kv/matches.
	_, err = sqlDB.Exec(`CREATE TABLE emp_items (
		group_id TEXT PRIMARY KEY, title TEXT NOT NULL, category TEXT NOT NULL DEFAULT '',
		category_id TEXT NOT NULL DEFAULT '', pub_date TIMESTAMP NOT NULL DEFAULT '',
		tags TEXT NOT NULL DEFAULT '', size_bytes INTEGER NOT NULL DEFAULT 0,
		filename TEXT NOT NULL DEFAULT '', infohash TEXT NOT NULL DEFAULT '',
		details_url TEXT NOT NULL DEFAULT '', enclosure_url TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT '', fetched_at TIMESTAMP NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL DEFAULT '')`)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open on legacy db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	it := Item{GroupID: "9", Title: "Legacy row", PubDate: time.Now().UTC(), FetchedAt: time.Now().UTC(), Seeders: 3}
	if _, err := db.UpsertItem(it); err != nil {
		t.Fatalf("upsert after migrate: %v", err)
	}
	got, err := db.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Seeders != 3 {
		t.Fatalf("migrated read = %+v", got)
	}
}

func TestPendingLifecycle(t *testing.T) {
	db := openTemp(t)
	id, err := db.AddPending(7, "Scene 8K", "1154515", "fuckpassvr-001", "Rainy City Rendezvous")
	if err != nil {
		t.Fatal(err)
	}
	pend, err := db.ListActivePending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) != 1 || pend[0].ID != id || pend[0].Status != "downloading" {
		t.Fatalf("pending = %+v", pend)
	}
	if err := db.NoteRescan(id); err != nil {
		t.Fatal(err)
	}
	if err := db.SetPendingStatus(id, "downloaded"); err != nil {
		t.Fatal(err)
	}
	pend, err = db.ListActivePending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) != 1 || pend[0].Rescans != 1 {
		t.Fatalf("after rescan = %+v", pend)
	}
	if err := db.SetPendingStatus(id, "linked"); err != nil {
		t.Fatal(err)
	}
	pend, err = db.ListActivePending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) != 0 {
		t.Fatalf("linked rows must drop out, got %+v", pend)
	}
}

func TestGetItem(t *testing.T) {
	db := openTemp(t)
	it := Item{GroupID: "1154515", Title: "Scene", PubDate: time.Now().UTC(), FetchedAt: time.Now().UTC()}
	if _, err := db.UpsertItem(it); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetItem("1154515")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Scene" {
		t.Errorf("title = %q", got.Title)
	}
	if _, err := db.GetItem("missing"); err == nil {
		t.Error("expected error for missing group, got nil")
	}
}
