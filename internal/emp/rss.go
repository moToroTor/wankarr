// Package emp ingests Emp torrent records and groups duplicate uploads of
// the same scene into comparable variants (resolution, HBR vs standard).
//
// Ingest channels are RSS notification feeds (polled on a slow schedule,
// the polite standing index) and Jackett/Torznab searches (explicit,
// on-demand backfill). All ranking and filtering happens locally.
package emp

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"wankarr/internal/store"
)

// feed mirrors the RSS 2.0 items Emp emits (Gazelle Feed Class), including
// the ezrss torrent extension carrying exact byte sizes.
type feed struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Items []feedItem `xml:"item"`
	} `xml:"channel"`
}

type feedItem struct {
	Title     string `xml:"title"`
	Link      string `xml:"link"`
	Category  string `xml:"category"`
	PubDate   string `xml:"pubDate"`
	Tags      string `xml:"tags"`
	Enclosure struct {
		URL string `xml:"url,attr"`
	} `xml:"enclosure"`
	// Size, hash, and filename live inside the nested ezrss extension:
	// <torrent><fileName/><infoHash/><contentLength/>…</torrent>.
	// They are NOT direct children of <item>; a flat mapping silently
	// yields zero sizes and empty filenames.
	Torrent struct {
		FileName      string `xml:"fileName"`
		InfoHash      string `xml:"infoHash"`
		ContentLength string `xml:"contentLength"`
	} `xml:"torrent"`
}

// groupIDRe extracts the torrent-group id from torrents.php?id=NNNN links.
var groupIDRe = regexp.MustCompile(`[?&]id=(\d+)`)

// GroupIDFromURL returns the Emp group id embedded in a details/download
// URL, or "" when the URL carries none.
func GroupIDFromURL(u string) string {
	m := groupIDRe.FindStringSubmatch(u)
	if m == nil {
		return ""
	}
	return m[1]
}

// ParseFeed converts raw RSS bytes into store items tagged with source.
// Malformed items are skipped, never fatal: feeds are third-party HTML.
func ParseFeed(raw []byte, source string, fetchedAt time.Time) ([]store.Item, error) {
	var f feed
	if err := xml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse rss: %w", err)
	}
	var out []store.Item
	for _, fi := range f.Channel.Items {
		gid := GroupIDFromURL(fi.Link)
		if gid == "" {
			continue
		}
		pub, err := time.Parse(time.RFC1123Z, strings.TrimSpace(fi.PubDate))
		if err != nil {
			continue
		}
		var size int64
		if s := strings.TrimSpace(fi.Torrent.ContentLength); s != "" {
			size, _ = strconv.ParseInt(s, 10, 64)
		}
		out = append(out, store.Item{
			GroupID:      gid,
			Title:        strings.TrimSpace(fi.Title),
			Category:     strings.TrimSpace(fi.Category),
			PubDate:      pub,
			Tags:         strings.Fields(strings.TrimSpace(fi.Tags)),
			SizeBytes:    size,
			Filename:     strings.TrimSpace(fi.Torrent.FileName),
			Infohash:     strings.TrimSpace(fi.Torrent.InfoHash),
			DetailsURL:   strings.TrimSpace(fi.Link),
			EnclosureURL: strings.TrimSpace(fi.Enclosure.URL),
			Source:       source,
			FetchedAt:    fetchedAt,
		})
	}
	return out, nil
}

// FetchFeed performs a conditional GET for one feed URL. Empty etag/mod
// means no prior poll. It returns nil items with hit=true on 304.
func FetchFeed(client *http.Client, url, etag, mod string) (items []byte, newEtag, newMod string, hit bool, err error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, "", "", false, err
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if mod != "" {
		req.Header.Set("If-Modified-Since", mod)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, "", "", false, fmt.Errorf("fetch feed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified {
		return nil, etag, mod, true, nil
	}
	if res.StatusCode != http.StatusOK {
		return nil, "", "", false, fmt.Errorf("fetch feed: status %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, "", "", false, err
	}
	return raw, res.Header.Get("ETag"), res.Header.Get("Last-Modified"), false, nil
}
