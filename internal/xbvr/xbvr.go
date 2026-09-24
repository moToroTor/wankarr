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
	"strconv"
	"strings"
	"time"
)

// Client talks to one XBVR instance. Timeout keeps a hung XBVR from
// stalling Wankarr's poll loop.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a client for baseURL like http://127.0.0.1:9999.
func NewClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 30 * time.Second}}
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
// Lenient: a missing or corrupt value yields nil; the authoritative
// merge in SeedFilenames re-reads and refuses to clobber corrupt data.
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

// ListOwned returns available scenes with their best local file height.
func (c *Client) ListOwned() ([]OwnedScene, error) {
	var out []OwnedScene
	const page = 200
	for offset := 0; ; offset += page {
		body, _ := json.Marshal(map[string]any{
			"isAvailable": true,
			"limit":       page,
			"offset":      offset,
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
			best := 0
			for _, f := range s.Files {
				if f.VideoHeight > best {
					best = f.VideoHeight
				}
			}
			out = append(out, OwnedScene{SceneID: s.SceneID, Title: s.Title, Site: s.Site, BestHeight: best})
		}
		if len(resp.Scenes) < page {
			break
		}
	}
	return out, nil
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
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
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

// SeedFilenames registers names (a grab's inner video-file basenames) on
// the scene's known-filenames list, so XBVR's next library scan
// auto-matches them even when the downloaded names differ from the
// scraped release names. It round-trips the full scene object: the edit
// endpoint overwrites every field it receives, so a partial update would
// wipe the rest. Returns how many names were added. A corrupt stored
// list is refused rather than clobbered.
func (c *Client) SeedFilenames(dbid uint, names []string) (int, error) {
	if dbid == 0 || len(names) == 0 {
		return 0, nil
	}
	var scene map[string]any
	if err := c.getJSON("/api/scene/"+strconv.FormatUint(uint64(dbid), 10), &scene); err != nil {
		return 0, err
	}
	var known []string
	if raw, ok := scene["filenames_arr"].(string); ok && strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &known); err != nil {
			return 0, fmt.Errorf("xbvr scene %d: filenames_arr: %w", dbid, err)
		}
	}
	have := map[string]bool{}
	for _, k := range known {
		have[k] = true
	}
	added := 0
	for _, n := range names {
		if n == "" || have[n] {
			continue
		}
		known = append(known, n)
		have[n] = true
		added++
	}
	if added == 0 {
		return 0, nil
	}
	enc, err := json.Marshal(known)
	if err != nil {
		return 0, err
	}
	scene["filenames_arr"] = string(enc)
	body, _ := json.Marshal(scene)
	var dst any
	if err := c.post("/api/scene/edit/"+strconv.FormatUint(uint64(dbid), 10), body, &dst); err != nil {
		return 0, err
	}
	return added, nil
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
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
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
