// Package torrent sends chosen torrents to a download client.
//
// v1 supports Transmission over its JSON-RPC protocol: session-id
// handshake, then torrent-add by URL. The interface keeps qBittorrent
// and Deluge addable without rewiring callers.
package torrent

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"time"
)

// Client is anything that can queue a torrent for download.
type Client interface {
	// AddURL queues the torrent at url (usually the tracker's
	// authenticated enclosure link). Returns the client-side name.
	AddURL(url string) (string, error)
	// AddFile queues raw .torrent bytes.
	AddFile(filename string, data []byte) (string, error)
}

// Transmission talks to Transmission's /transmission/rpc endpoint.
type Transmission struct {
	rpcURL string
	user   string
	pass   string
	// Optional download directory sent as torrent-add "download-dir".
	// Empty means omit it and use the server default.
	downloadDir string
	http        *http.Client
	sessID      string
}

// NewTransmission returns a client for rpcURL like
// http://host:9091/transmission/rpc.
func NewTransmission(rpcURL, user, pass string) *Transmission {
	return &Transmission{rpcURL: rpcURL, user: user, pass: pass,
		http: &http.Client{Timeout: 60 * time.Second}}
}

// NewTransmissionWithDir is NewTransmission plus an explicit download
// directory (TRANSMISSION_DOWNLOAD_DIR). Empty dir behaves identically
// to NewTransmission.
func NewTransmissionWithDir(rpcURL, user, pass, downloadDir string) *Transmission {
	return &Transmission{rpcURL: rpcURL, user: user, pass: pass, downloadDir: downloadDir,
		http: &http.Client{Timeout: 60 * time.Second}}
}

type rpcRequest struct {
	Method    string         `json:"method"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Tag       int            `json:"tag,omitempty"`
}

type rpcResponse struct {
	Result    string `json:"result"`
	Arguments struct {
		TorrentAdded struct {
			Name string `json:"name"`
			ID   int    `json:"id"`
		} `json:"torrent-added"`
		TorrentDuplicate struct {
			Name string `json:"name"`
			ID   int    `json:"id"`
		} `json:"torrent-duplicate"`
		Torrents []TorrentStatus `json:"torrents"`
	} `json:"arguments"`
}

// TorrentStatus is the subset of torrent-get fields Wankarr polls to
// detect completion. Status follows Transmission's codes (4 =
// downloading, 6 = seeding); Done is true for a finished torrent.
type TorrentStatus struct {
	ID          int           `json:"id"`
	Name        string        `json:"name"`
	PercentDone float64       `json:"percentDone"`
	Status      int           `json:"status"`
	IsFinished  bool          `json:"isFinished"`
	Files       []TorrentFile `json:"files"`
}

// TorrentFile is one file inside a torrent. Name is torrent-root-relative
// ("folder/video.mp4"); FileNames reduces entries to basenames.
type TorrentFile struct {
	Name   string `json:"name"`
	Length int64  `json:"length"`
}

// Done reports a completed torrent: fully downloaded or seeding.
func (s TorrentStatus) Done() bool {
	return s.IsFinished || s.PercentDone >= 1 || s.Status == 6
}

func (t *Transmission) call(method string, args map[string]any) (*rpcResponse, error) {
	payload, _ := json.Marshal(rpcRequest{Method: method, Arguments: args})
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequest(http.MethodPost, t.rpcURL, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if t.sessID != "" {
			req.Header.Set("X-Transmission-Session-Id", t.sessID)
		}
		if t.user != "" {
			req.SetBasicAuth(t.user, t.pass)
		}
		res, err := t.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("transmission: %w", err)
		}
		raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		if res.StatusCode == http.StatusConflict {
			t.sessID = res.Header.Get("X-Transmission-Session-Id")
			if t.sessID == "" {
				return nil, fmt.Errorf("transmission: 409 without session id")
			}
			continue
		}
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("transmission: status %d", res.StatusCode)
		}
		var out rpcResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("transmission: decode: %w", err)
		}
		if out.Result != "success" {
			return nil, fmt.Errorf("transmission: %s", out.Result)
		}
		return &out, nil
	}
	return nil, fmt.Errorf("transmission: session handshake failed")
}

// AddURL queues the torrent at url. Returns the client-side name and the
// Transmission torrent ID (for completion polling).
func (t *Transmission) AddURL(url string) (name string, id int, err error) {
	args := map[string]any{"filename": url}
	if t.downloadDir != "" {
		args["download-dir"] = t.downloadDir
	}
	out, err := t.call("torrent-add", args)
	if err != nil {
		return "", 0, err
	}
	if out.Arguments.TorrentDuplicate.Name != "" {
		return out.Arguments.TorrentDuplicate.Name + " (already queued)", out.Arguments.TorrentDuplicate.ID, nil
	}
	return out.Arguments.TorrentAdded.Name, out.Arguments.TorrentAdded.ID, nil
}

// AddFile queues raw .torrent bytes.
func (t *Transmission) AddFile(filename string, data []byte) (string, error) {
	args := map[string]any{
		"metainfo": base64.StdEncoding.EncodeToString(data),
	}
	if t.downloadDir != "" {
		args["download-dir"] = t.downloadDir
	}
	out, err := t.call("torrent-add", args)
	if err != nil {
		return "", err
	}
	if out.Arguments.TorrentDuplicate.Name != "" {
		return out.Arguments.TorrentDuplicate.Name + " (already queued)", nil
	}
	return out.Arguments.TorrentAdded.Name, nil
}

// ErrTorrentNotFound is returned by StatusOf when Transmission knows no
// torrent with that ID (removed client-side, or a stale pending row).
var ErrTorrentNotFound = fmt.Errorf("transmission: no such torrent")

// StatusOf reports one torrent's progress for completion polling.
func (t *Transmission) StatusOf(id int) (TorrentStatus, error) {
	out, err := t.call("torrent-get", map[string]any{
		"ids":    []int{id},
		"fields": []string{"id", "name", "percentDone", "status", "isFinished"},
	})
	if err != nil {
		return TorrentStatus{}, err
	}
	if len(out.Arguments.Torrents) == 0 {
		return TorrentStatus{}, ErrTorrentNotFound
	}
	return out.Arguments.Torrents[0], nil
}

// FileNames lists a torrent's inner file basenames ("folder/video.mp4"
// becomes "video.mp4"). Used to register downloaded names on the XBVR
// scene so the next library scan auto-matches them.
func (t *Transmission) FileNames(id int) ([]string, error) {
	out, err := t.call("torrent-get", map[string]any{
		"ids":    []int{id},
		"fields": []string{"files"},
	})
	if err != nil {
		return nil, err
	}
	if len(out.Arguments.Torrents) == 0 {
		return nil, ErrTorrentNotFound
	}
	var names []string
	for _, f := range out.Arguments.Torrents[0].Files {
		if base := path.Base(f.Name); base != "" && base != "." && base != "/" {
			names = append(names, base)
		}
	}
	return names, nil
}
