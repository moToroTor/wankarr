// Package xbvr reads library data from XBVR's REST API (read-only).
//
// It sources Wankarr's work queue: wishlisted scenes (wanted, not owned)
// and owned scenes with their local file quality for upgrade comparison.
package xbvr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client talks to one XBVR instance. Timeout keeps a hung XBVR from
// stalling Wankarr's poll loop.
type Client struct {
	baseURL string
	http    *http.Client
	// httpLong serves the synchronous scrape-pick call, which runs one
	// search plus one page fetch server-side (~30s).
	httpLong *http.Client
}

// NewClient returns a client for baseURL like http://127.0.0.1:9999.
func NewClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 30 * time.Second}, httpLong: &http.Client{Timeout: 90 * time.Second}}
}

// ScrapeCandidate is one ranked scrape target: enough for a human to
// say "this is the best one".
type ScrapeCandidate struct {
	ScraperID   string `json:"scraper_id"`
	ScraperName string `json:"scraper_name"`
	Domain      string `json:"domain"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Reason      string `json:"reason"`
	Preferred   bool   `json:"preferred"`
}

// ScrapePickResult is the scrape-pick outcome. SceneID 0 means nothing
// found or the page yielded no scene — the caller falls back to the
// scrape-search samples.
type ScrapePickResult struct {
	Status     string          `json:"status"`
	SceneID    uint            `json:"scene_id"`
	Wishlisted bool            `json:"wishlisted"`
	Candidate  ScrapeCandidate `json:"candidate"`
}

// SearchScenes returns XBVR scenes matching a title query.
func (c *Client) SearchScenes(q string) ([]Scene, error) {
	var resp struct {
		Scenes []Scene `json:"scenes"`
	}
	if err := c.getJSON("/api/scene/search?q="+url.QueryEscape(q), &resp); err != nil {
		return nil, err
	}
	return resp.Scenes, nil
}

// GetScene reads one scene by numeric id (wishlist flag, availability).
func (c *Client) GetScene(id uint) (Scene, error) {
	var s Scene
	if err := c.getJSON("/api/scene/"+strconv.FormatUint(uint64(id), 10), &s); err != nil {
		return Scene{}, err
	}
	return s, nil
}

// ScrapeSearch returns ranked scrape candidates for a query string.
// Read-only: nothing is scraped or stored.
func (c *Client) ScrapeSearch(q string) ([]ScrapeCandidate, error) {
	body, _ := json.Marshal(map[string]any{"q": q})
	var resp struct {
		Status     string            `json:"status"`
		Candidates []ScrapeCandidate `json:"candidates"`
	}
	if err := c.post("/api/task/scrape-search", body, &resp); err != nil {
		return nil, err
	}
	if resp.Status != "OK" {
		return nil, fmt.Errorf("xbvr scrape-search: status %q", resp.Status)
	}
	return resp.Candidates, nil
}

// ScrapePick scrapes one candidate (or the ranked best when scraperID
// and url are empty) and optionally wishlists the persisted scene.
// Trust the returned Wishlisted flag over an immediate re-read: the
// write is durable before the response, but a fresh GET in the same
// tick can still serve the pre-write value.
func (c *Client) ScrapePick(title, site string, performers []string, scraperID, url string, wishlist bool) (ScrapePickResult, error) {
	if performers == nil {
		performers = []string{}
	}
	body, _ := json.Marshal(map[string]any{
		"title": title, "site": site, "performers": performers,
		"scraper_id": scraperID, "url": url, "wishlist": wishlist,
	})
	var res ScrapePickResult
	if err := c.postLong("/api/task/scrape-pick", body, &res); err != nil {
		return ScrapePickResult{}, err
	}
	if res.Status != "OK" {
		return ScrapePickResult{}, fmt.Errorf("xbvr scrape-pick: status %q", res.Status)
	}
	return res, nil
}

// ToggleWishlist flips a scene's wishlist flag. It is a TOGGLE: read
// the scene first and call only when Wishlist is false — and never on
// library scenes (the server refuses those).
func (c *Client) ToggleWishlist(sceneID string) error {
	body, _ := json.Marshal(map[string]any{"scene_id": sceneID, "list": "wishlist"})
	var dst any
	return c.post("/api/scene/toggle", body, &dst)
}

// File mirrors the XBVR file fields Wankarr needs for quality comparison
// and post-download linking.
type File struct {
	ID             uint    `json:"id"`
	SceneID        uint    `json:"scene_id"`
	Filename       string  `json:"filename"`
	VideoWidth     int     `json:"video_width"`
	VideoHeight    int     `json:"video_height"`
	VideoBitRate   int     `json:"video_bitrate"`
	VideoCodecName string  `json:"video_codec_name"`
	VideoDuration  float64 `json:"duration"`
	// Size is the file's bytes on disk, for byte-equality matching
	// against Emp upload sizes (0 when XBVR doesn't report it).
	Size int64 `json:"size"`
}

// Actor mirrors the XBVR performer fields Wankarr needs.
type Actor struct {
	Name string `json:"name"`
}

// Scene mirrors the XBVR scene fields Wankarr needs.
type Scene struct {
	// ID is XBVR's numeric database key (the edit endpoint's path).
	// Encoding/json matches it case-insensitively on decode.
	ID           uint    `json:"id"`
	SceneID      string  `json:"scene_id"`
	Title        string  `json:"title"`
	Site         string  `json:"site"`
	Studio       string  `json:"studio"`
	ReleaseDate  string  `json:"release_date"`
	CoverURL     string  `json:"cover_url"`
	Cast         []Actor `json:"cast"`
	IsAvailable  bool    `json:"is_available"`
	Wishlist     bool    `json:"wishlist"`
	Files        []File  `json:"file"`
	FilenamesArr string  `json:"filenames_arr"`
}

// WantedScene is a wishlist entry: something to find on Emp.
type WantedScene struct {
	SceneID    string
	Title      string
	Site       string
	Studio     string
	CoverURL   string
	Performers []string
	// DBID is XBVR's numeric key for filename seeding (0 when unknown).
	DBID uint
	// KnownFilenames is the scene's scraped known-filenames snapshot.
	KnownFilenames []string
}

// parseFilenamesArr decodes XBVR's JSON-encoded known-filenames string.
// Lenient: a missing or corrupt value yields nil.
func parseFilenamesArr(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// OwnedScene pairs a library scene with its best local resolution height
// (0 when the scene has no matched files yet).
type OwnedScene struct {
	SceneID    string
	Title      string
	Site       string
	BestHeight int
	// Sizes holds the byte sizes of the scene's matched files, for
	// byte-equality rescue of title matches (sizeless files omitted).
	Sizes []int64
	// CoverURL rides the same snapshot query so matched groups can
	// show XBVR artwork with no extra request.
	CoverURL string
}

// ListWishlist returns all wishlisted scenes, following pagination.
func (c *Client) ListWishlist() ([]WantedScene, error) {
	var out []WantedScene
	const page = 200
	for offset := 0; ; offset += page {
		body, _ := json.Marshal(map[string]any{
			"lists":  []string{"wishlist"},
			"limit":  page,
			"offset": offset,
		})
		var resp struct {
			Scenes []Scene `json:"scenes"`
		}
		if err := c.post("/api/scene/list", body, &resp); err != nil {
			return nil, err
		}
		if len(resp.Scenes) == 0 {
			break
		}
		for _, s := range resp.Scenes {
			var performers []string
			for _, a := range s.Cast {
				if a.Name != "" {
					performers = append(performers, a.Name)
				}
			}
			out = append(out, WantedScene{SceneID: s.SceneID, Title: s.Title, Site: s.Site, Studio: s.Studio, CoverURL: s.CoverURL, Performers: performers, DBID: s.ID, KnownFilenames: parseFilenamesArr(s.FilenamesArr)})
		}
		if len(resp.Scenes) < page {
			break
		}
	}
	return out, nil
}

// ownedPageSize matches the previous sequential paging.
// ownedFetchWorkers bounds concurrent page fetches: XBVR answers each
// page with a file-joining query, so unbounded parallelism would just
// move the queue onto XBVR.
const ownedPageSize = 200

const ownedFetchWorkers = 4

// ListOwned returns available scenes with their best local file height.
// Offset 0 fetches first: small libraries finish in one round trip,
// exactly as before. A full first page fans out for the remaining
// offsets, merging in offset order so large libraries don't pay a full
// round trip per page. A short or empty page ends the scan; any page
// error fails the whole snapshot (the caller serves stale instead).
func (c *Client) ListOwned() ([]OwnedScene, error) {
	first, err := c.fetchOwnedPage(0)
	if err != nil {
		return nil, err
	}
	if len(first) < ownedPageSize {
		return mapOwnedScenes(first), nil
	}
	var mu sync.Mutex
	next := ownedPageSize
	stopped := false
	pages := map[int][]Scene{0: first}
	var firstErr error
	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for {
			mu.Lock()
			if stopped || firstErr != nil {
				mu.Unlock()
				return
			}
			offset := next
			next += ownedPageSize
			mu.Unlock()
			scenes, err := c.fetchOwnedPage(offset)
			mu.Lock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			pages[offset] = scenes
			if len(scenes) < ownedPageSize {
				stopped = true
			}
			mu.Unlock()
		}
	}
	wg.Add(ownedFetchWorkers)
	for i := 0; i < ownedFetchWorkers; i++ {
		go worker()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	var offsets []int
	for offset := range pages {
		offsets = append(offsets, offset)
	}
	sort.Ints(offsets)
	var out []OwnedScene
	for _, offset := range offsets {
		out = append(out, mapOwnedScenes(pages[offset])...)
	}
	return out, nil
}

// mapOwnedScenes reduces raw scenes to the owned fields Wankarr keeps.
func mapOwnedScenes(scenes []Scene) []OwnedScene {
	var out []OwnedScene
	for _, s := range scenes {
		best := 0
		var sizes []int64
		for _, f := range s.Files {
			if f.VideoHeight > best {
				best = f.VideoHeight
			}
			if f.Size > 0 {
				sizes = append(sizes, f.Size)
			}
		}
		out = append(out, OwnedScene{SceneID: s.SceneID, Title: s.Title, Site: s.Site, BestHeight: best, Sizes: sizes, CoverURL: s.CoverURL})
	}
	return out
}

// fetchOwnedPage returns one raw library page at the given offset.
func (c *Client) fetchOwnedPage(offset int) ([]Scene, error) {
	body, _ := json.Marshal(map[string]any{
		"isAvailable": true,
		"limit":       ownedPageSize,
		"offset":      offset,
	})
	var resp struct {
		Scenes []Scene `json:"scenes"`
	}
	if err := c.post("/api/scene/list", body, &resp); err != nil {
		return nil, err
	}
	return resp.Scenes, nil
}

// Rescan triggers XBVR's library rescan (async server-side): new files
// appearing in watched volumes are picked up and auto-matched to scenes
// by filename. It returns once the task is queued, not when it finishes.
func (c *Client) Rescan() error {
	return c.get("/api/task/rescan")
}

// FindFile looks up XBVR files whose filename contains name (usually the
// Transmission torrent name). It prefers unmatched files and reports
// whether anything was found at all.
func (c *Client) FindFile(name string) (File, bool, error) {
	body, _ := json.Marshal(map[string]any{"filename": name})
	var files []File
	if err := c.post("/api/files/list", body, &files); err != nil {
		return File{}, false, err
	}
	if len(files) == 0 {
		return File{}, false, nil
	}
	for _, f := range files {
		if f.SceneID == 0 {
			return f, true, nil
		}
	}
	return files[0], true, nil
}

// MatchFile explicitly links an XBVR file to a scene. Used when the
// rescan's filename auto-match leaves a wishlist grab unlinked.
func (c *Client) MatchFile(sceneID string, fileID uint) error {
	body, _ := json.Marshal(map[string]any{"scene_id": sceneID, "file_id": fileID})
	var dst any
	return c.post("/api/files/match", body, &dst)
}

// SeedFilenames registers names (a grab's inner video-file basenames) on
// the scene's known-filenames list, so XBVR's next library scan
// auto-matches them even when the downloaded names differ from the
// scraped release names. It uses XBVR's additive filenames endpoint,
// which merges server-side with exact-match dedupe — no read-modify-write
// of the full scene object. Returns how many names were sent; the server
// dedupes, so re-sends are harmless no-ops. Any non-200 status (unknown
// scene, corrupt stored list, missing endpoint) is an error.
func (c *Client) SeedFilenames(dbid uint, names []string) (int, error) {
	if dbid == 0 || len(names) == 0 {
		return 0, nil
	}
	path := "/api/scene/filenames/" + strconv.FormatUint(uint64(dbid), 10)
	body, _ := json.Marshal(map[string]any{"filenames": names})
	var merged []string
	if err := c.post(path, body, &merged); err != nil {
		return 0, err
	}
	return len(names), nil
}

func (c *Client) get(path string) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("xbvr GET %s: %w", path, err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("xbvr GET %s: status %d", path, res.StatusCode)
	}
	return nil
}

func (c *Client) post(path string, body []byte, dst any) error {
	return c.doPost(c.http, path, body, dst)
}

// postLong is post over the long-timeout client, for synchronous calls
// that run a search plus a page fetch server-side.
func (c *Client) postLong(path string, body []byte, dst any) error {
	return c.doPost(c.httpLong, path, body, dst)
}

func (c *Client) doPost(hc *http.Client, path string, body []byte, dst any) error {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("xbvr POST %s: %w", path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("xbvr POST %s: status %d", path, res.StatusCode)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("xbvr POST %s: decode: %w", path, err)
	}
	return nil
}

func (c *Client) getJSON(path string, dst any) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("xbvr GET %s: %w", path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("xbvr GET %s: status %d", path, res.StatusCode)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("xbvr GET %s: decode: %w", path, err)
	}
	return nil
}
