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

// File mirrors the XBVR file fields Wankarr needs for quality comparison.
type File struct {
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
	SceneID     string  `json:"scene_id"`
	Title       string  `json:"title"`
	Site        string  `json:"site"`
	Studio      string  `json:"studio"`
	ReleaseDate string  `json:"release_date"`
	CoverURL    string  `json:"cover_url"`
	Cast        []Actor `json:"cast"`
	IsAvailable bool    `json:"is_available"`
	Wishlist    bool    `json:"wishlist"`
	Files       []File  `json:"file"`
}

// WantedScene is a wishlist entry: something to find on Emp.
type WantedScene struct {
	SceneID    string
	Title      string
	Site       string
	Studio     string
	CoverURL   string
	Performers []string
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
			out = append(out, WantedScene{SceneID: s.SceneID, Title: s.Title, Site: s.Site, Studio: s.Studio, CoverURL: s.CoverURL, Performers: performers})
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
