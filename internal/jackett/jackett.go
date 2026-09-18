// Package jackett queries a local Jackett server over Torznab for
// explicit, on-demand Emp searches (backfill and old scenes).
//
// Torznab is a query API with structured results — keyword search,
// sizes, and seed health — unlike the flat notification RSS feeds.
// Every search here is user-triggered; Wankarr never polls Jackett.
package jackett

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"wankarr/internal/store"
)

// Client talks to one Jackett server's Torznab endpoint.
type Client struct {
	baseURL string
	apiKey  string
	empPath string
	http    *http.Client
}

// NewClient builds a Torznab client. baseURL is the Jackett server,
// empPath the Empornium indexer Torznab path.
func NewClient(baseURL, apiKey, empPath string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, empPath: empPath,
		http: &http.Client{Timeout: 60 * time.Second},
	}
}

type torznabFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Items []torznabItem `xml:"item"`
	} `xml:"channel"`
}

type torznabItem struct {
	Title       string `xml:"title"`
	GUID        string `xml:"guid"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
	Size        string `xml:"size"`
	Description string `xml:"description"`
	Enclosure   struct {
		URL    string `xml:"url,attr"`
		Length string `xml:"length,attr"`
	} `xml:"enclosure"`
	Attrs []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	} `xml:"attr"`
}

// Search runs one Torznab query and maps results to store items.
// Group IDs come from the details link (torrents.php?id=NNNN).
func (c *Client) Search(query string, groupIDOf func(string) string) ([]store.Item, error) {
	u := c.baseURL + c.empPath + "?t=search&q=" + url.QueryEscape(query) + "&apikey=" + url.QueryEscape(c.apiKey)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jackett search: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jackett search: status %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	return ParseTorznab(raw, groupIDOf)
}

// ParseTorznab converts raw Torznab XML into store items (source jackett).
func ParseTorznab(raw []byte, groupIDOf func(string) string) ([]store.Item, error) {
	var f torznabFeed
	if err := xml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse torznab: %w", err)
	}
	now := time.Now().UTC()
	var out []store.Item
	for _, ti := range f.Channel.Items {
		gid := groupIDOf(ti.Link)
		if gid == "" {
			gid = groupIDOf(ti.GUID)
		}
		if gid == "" {
			continue
		}
		attrs := map[string]string{}
		for _, a := range ti.Attrs {
			attrs[a.Name] = a.Value
		}
		var size int64
		for _, s := range []string{strings.TrimSpace(ti.Size), attrs["size"], strings.TrimSpace(ti.Enclosure.Length)} {
			if s == "" {
				continue
			}
			if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > size {
				size = n
			}
		}
		pub := now
		if p, err := time.Parse(time.RFC1123Z, strings.TrimSpace(ti.PubDate)); err == nil {
			pub = p
		}
		// Empornium's Cardigann definition maps <description> to
		// "Verified: <tag list>" / "Unverified: <tag list>", so the tag
		// vocabulary arrives inside the description text.
		tags := parseEmpTags(ti.Description)
		if cat := attrs["category"]; cat != "" {
			tags = append(tags, "cat:"+cat)
		}
		var seeders int
		if s := attrs["seeders"]; s != "" {
			seeders, _ = strconv.Atoi(s)
		}
		cover := attrs["coverurl"]
		if cover == "" {
			cover = attrs["poster"]
		}
		out = append(out, store.Item{
			GroupID:      gid,
			Title:        strings.TrimSpace(ti.Title),
			Category:     attrs["category"],
			PubDate:      pub,
			Tags:         tags,
			SizeBytes:    size,
			Seeders:      seeders,
			Freeleech:    attrs["downloadvolumefactor"] == "0",
			CoverURL:     strings.TrimSpace(cover),
			DetailsURL:   strings.TrimSpace(ti.Link),
			EnclosureURL: strings.TrimSpace(ti.Enclosure.URL),
			Source:       "jackett",
			FetchedAt:    now,
		})
	}
	return out, nil
}

// parseEmpTags extracts the tag list from an Empornium-style description
// ("Verified: tag1 tag2 …" / "Unverified: tag1 tag2 …"). Anything else
// yields no tags rather than garbage.
func parseEmpTags(desc string) []string {
	d := strings.TrimSpace(desc)
	for _, prefix := range []string{"Verified:", "Unverified:"} {
		if strings.HasPrefix(d, prefix) {
			return strings.Fields(strings.TrimSpace(strings.TrimPrefix(d, prefix)))
		}
	}
	return nil
}
