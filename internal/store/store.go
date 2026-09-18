// Package store owns Wankarr's local SQLite database: Emp items collected
// from RSS feeds and Jackett searches, plus wishlist-match state.
//
// All browsing, filtering, and comparing happens against this store, so
// Emp sees traffic only at ingest time. Credential-bearing fields
// (notably EnclosureURL, which embeds the user's authkey/torrent_pass)
// are stored for download use but must never be logged or rendered.
package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Item is one Emp torrent record keyed by its torrent-group ID.
type Item struct {
	GroupID      string
	Title        string
	Category     string
	CategoryID   string
	PubDate      time.Time
	Tags         []string
	SizeBytes    int64
	Seeders      int
	Freeleech    bool
	CoverURL     string
	Filename     string
	Infohash     string
	DetailsURL   string
	EnclosureURL string
	Source       string // "rss" or "jackett"
	FetchedAt    time.Time
}

// Schema applied on open. Keep migrations additive; this is v1.
const schema = `
CREATE TABLE IF NOT EXISTS emp_items (
	group_id     TEXT PRIMARY KEY,
	title        TEXT NOT NULL,
	category     TEXT NOT NULL DEFAULT '',
	category_id  TEXT NOT NULL DEFAULT '',
	pub_date     TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
	tags         TEXT NOT NULL DEFAULT '',
	size_bytes   INTEGER NOT NULL DEFAULT 0,
	seeders      INTEGER NOT NULL DEFAULT 0,
	freeleech    INTEGER NOT NULL DEFAULT 0,
	cover_url    TEXT NOT NULL DEFAULT '',
	filename     TEXT NOT NULL DEFAULT '',
	infohash     TEXT NOT NULL DEFAULT '',
	details_url  TEXT NOT NULL DEFAULT '',
	enclosure_url TEXT NOT NULL DEFAULT '',
	source       TEXT NOT NULL DEFAULT '',
	fetched_at   TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
	created_at   TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_emp_items_pub_date ON emp_items(pub_date DESC);
CREATE INDEX IF NOT EXISTS idx_emp_items_source ON emp_items(source);

CREATE TABLE IF NOT EXISTS wishlist_matches (
	group_key  TEXT NOT NULL,
	scene_id   TEXT NOT NULL,
	score      REAL NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
	dismissed  INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (group_key, scene_id)
);
CREATE INDEX IF NOT EXISTS idx_matches_scene ON wishlist_matches(scene_id);

CREATE TABLE IF NOT EXISTS kv (
	key        TEXT PRIMARY KEY,
	value      TEXT NOT NULL DEFAULT '',
	updated_at TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
`

// DB wraps the SQLite handle.
type DB struct {
	sql *sql.DB
}

// Open creates the database file if needed and applies the schema.
func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	sqlDB.SetMaxOpenConns(1)
	if _, err := sqlDB.Exec(schema); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	// Additive migrations for databases created before a column existed.
	// SQLite lacks IF NOT EXISTS for ADD COLUMN, so probe first.
	for _, col := range []string{"seeders INTEGER NOT NULL DEFAULT 0", "freeleech INTEGER NOT NULL DEFAULT 0", "cover_url TEXT NOT NULL DEFAULT ''"} {
		name := strings.Fields(col)[0]
		var exists bool
		err := sqlDB.QueryRow(`SELECT EXISTS(SELECT 1 FROM pragma_table_info('emp_items') WHERE name = ?)`, name).Scan(&exists)
		if err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("probe column %s: %w", name, err)
		}
		if !exists {
			if _, err := sqlDB.Exec(`ALTER TABLE emp_items ADD COLUMN ` + col); err != nil {
				sqlDB.Close()
				return nil, fmt.Errorf("add column %s: %w", name, err)
			}
		}
	}
	return &DB{sql: sqlDB}, nil
}

// Close releases the database handle.
func (d *DB) Close() error { return d.sql.Close() }

// UpsertItem inserts or replaces an item by group ID. Returns true when the
// row is new (first time this group ID was seen).
func (d *DB) UpsertItem(it Item) (bool, error) {
	var existed bool
	err := d.sql.QueryRow(`SELECT EXISTS(SELECT 1 FROM emp_items WHERE group_id = ?)`, it.GroupID).Scan(&existed)
	if err != nil {
		return false, err
	}
	freeleech := 0
	if it.Freeleech {
		freeleech = 1
	}
	_, err = d.sql.Exec(`
INSERT INTO emp_items (group_id, title, category, category_id, pub_date, tags,
	size_bytes, seeders, freeleech, cover_url, filename, infohash, details_url, enclosure_url, source, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(group_id) DO UPDATE SET
	title=excluded.title, category=excluded.category, category_id=excluded.category_id,
	pub_date=excluded.pub_date, tags=excluded.tags, size_bytes=excluded.size_bytes,
	seeders=excluded.seeders, freeleech=excluded.freeleech, cover_url=excluded.cover_url,
	filename=excluded.filename, infohash=excluded.infohash, details_url=excluded.details_url,
	enclosure_url=excluded.enclosure_url, source=excluded.source, fetched_at=excluded.fetched_at`,
		it.GroupID, it.Title, it.Category, it.CategoryID, it.PubDate.UTC().Format(time.RFC3339),
		strings.Join(it.Tags, " "), it.SizeBytes, it.Seeders, freeleech, it.CoverURL, it.Filename, it.Infohash,
		it.DetailsURL, it.EnclosureURL, it.Source, it.FetchedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return false, err
	}
	return !existed, nil
}

// Count returns the number of stored items.
func (d *DB) Count() (int, error) {
	var n int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM emp_items`).Scan(&n)
	return n, err
}

// Match is a persisted wishlist pairing.
type Match struct {
	GroupKey  string
	SceneID   string
	Score     float64
	CreatedAt time.Time
	Dismissed bool
}

// SaveMatches records pairings, keeping the best score per group/scene.
func (d *DB) SaveMatches(ms []Match) error {
	for _, m := range ms {
		_, err := d.sql.Exec(`
INSERT INTO wishlist_matches (group_key, scene_id, score) VALUES (?, ?, ?)
ON CONFLICT(group_key, scene_id) DO UPDATE SET score=MAX(score, excluded.score)`,
			m.GroupKey, m.SceneID, m.Score)
		if err != nil {
			return err
		}
	}
	return nil
}

// ListMatches returns recent non-dismissed matches, newest first.
func (d *DB) ListMatches(limit int) ([]Match, error) {
	rows, err := d.sql.Query(`
SELECT group_key, scene_id, score, created_at, dismissed
FROM wishlist_matches WHERE dismissed = 0 ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Match{}
	for rows.Next() {
		var m Match
		var created string
		var dismissed int
		if err := rows.Scan(&m.GroupKey, &m.SceneID, &m.Score, &created, &dismissed); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, created)
		m.Dismissed = dismissed != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

// Get returns all stored items, newest first, up to limit (0 = all).
func (d *DB) Get(limit int) ([]Item, error) {
	q := `SELECT group_id, title, category, category_id, pub_date, tags, size_bytes,
	seeders, freeleech, cover_url, filename, infohash, details_url, source, fetched_at
	FROM emp_items ORDER BY pub_date DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		rows, err := d.sql.Query(q, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanItems(rows)
	}
	rows, err := d.sql.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

// EnclosureURL returns the credential-bearing download URL for one group.
// It is a dedicated lookup so bulk queries never touch secrets; callers
// must not log or render the result.
func (d *DB) EnclosureURL(groupID string) (string, error) {
	var u string
	err := d.sql.QueryRow(`SELECT enclosure_url FROM emp_items WHERE group_id = ?`, groupID).Scan(&u)
	if err != nil {
		return "", err
	}
	return u, nil
}

// KV helpers persist poll state (ETags) across restarts.

// GetKV returns the value for key, or "" when absent.
func (d *DB) GetKV(key string) string {
	var v string
	_ = d.sql.QueryRow(`SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	return v
}

// SetKV upserts a key/value pair.
func (d *DB) SetKV(key, value string) error {
	_, err := d.sql.Exec(`
INSERT INTO kv (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now')`,
		key, value)
	return err
}

// Recent returns the newest items, newest first, up to limit.
func (d *DB) Recent(limit int) ([]Item, error) {
	rows, err := d.sql.Query(`
SELECT group_id, title, category, category_id, pub_date, tags, size_bytes,
	seeders, freeleech, cover_url, filename, infohash, details_url, source, fetched_at
FROM emp_items ORDER BY pub_date DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

func scanItems(rows *sql.Rows) ([]Item, error) {
	var out []Item
	for rows.Next() {
		var it Item
		var pub, fetched, tags string
		var freeleech int
		if err := rows.Scan(&it.GroupID, &it.Title, &it.Category, &it.CategoryID,
			&pub, &tags, &it.SizeBytes, &it.Seeders, &freeleech, &it.CoverURL, &it.Filename, &it.Infohash,
			&it.DetailsURL, &it.Source, &fetched); err != nil {
			return nil, err
		}
		it.Freeleech = freeleech != 0
		it.PubDate, _ = time.Parse(time.RFC3339, pub)
		it.FetchedAt, _ = time.Parse(time.RFC3339, fetched)
		if tags != "" {
			it.Tags = strings.Split(tags, " ")
		}
		// EnclosureURL is deliberately not selected here: callers that
		// need it (download path) must opt in via a dedicated lookup.
		out = append(out, it)
	}
	return out, rows.Err()
}
