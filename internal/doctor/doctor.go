// Package doctor implements `wankarr doctor`: a preflight check that
// validates configuration and verifies connectivity to every configured
// host. Secrets are never printed; feed URLs are reduced to their host.
package doctor

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"wankarr/internal/config"
)

// result is one check line.
type result struct {
	name   string
	ok     bool
	detail string
}

// Run executes all checks, prints them, and returns a process exit code
// (0 when everything required passes).
func Run(cfg *config.Config) int {
	client := &http.Client{Timeout: 10 * time.Second}
	var rs []result

	rs = append(rs, result{"config", true, "loaded"})

	rs = append(rs, checkHTTP(client, "xbvr", cfg.XBVRURL+"/api/scene/filters"))
	if cfg.JackettURL == "" || cfg.JackettAPIKey == "" {
		rs = append(rs, result{"jackett", false, "JACKETT_URL or JACKETT_API_KEY unset — searches will fail"})
	} else {
		rs = append(rs, checkHTTP(client, "jackett", cfg.JackettURL))
	}
	rs = append(rs, checkTransmission(client, cfg.TransmissionURL, cfg.TransmissionUser, cfg.TransmissionPass))
	if len(cfg.RSSFeeds) == 0 {
		rs = append(rs, result{"rss", false, "no RSS_FEEDS configured — index will stay empty"})
	}
	for _, f := range cfg.RSSFeeds {
		rs = append(rs, checkFeed(client, f))
	}

	code := 0
	for _, r := range rs {
		status := "ok"
		if !r.ok {
			status = "FAIL"
			code = 1
		}
		fmt.Printf("%-12s %-4s %s\n", r.name, status, r.detail)
	}
	return code
}

// checkHTTP GETs url and accepts any sub-500 status as "host reachable".
func checkHTTP(client *http.Client, name, url string) result {
	res, err := client.Get(url)
	if err != nil {
		return result{name, false, err.Error()}
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode >= 500 {
		return result{name, false, fmt.Sprintf("HTTP %d", res.StatusCode)}
	}
	return result{name, true, fmt.Sprintf("HTTP %d", res.StatusCode)}
}

// checkTransmission posts a session probe: Transmission answers 409 with
// a session id when RPC is alive (or 200 when a session is already known).
func checkTransmission(client *http.Client, rpcURL, user, pass string) result {
	auth := ""
	if user != "" {
		// Length only: lets you confirm the loaded value matches what
		// you typed, without ever printing the secret.
		auth = fmt.Sprintf(" (user %q, passlen %d)", user, len(pass))
	}
	req, err := http.NewRequest(http.MethodPost, rpcURL, strings.NewReader(`{"method":"session_get"}`))
	if err != nil {
		return result{"transmission", false, err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	res, err := client.Do(req)
	if err != nil {
		return result{"transmission", false, err.Error() + auth}
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode == http.StatusConflict || res.StatusCode == http.StatusOK {
		return result{"transmission", true, fmt.Sprintf("RPC alive (HTTP %d)%s", res.StatusCode, auth)}
	}
	return result{"transmission", false, fmt.Sprintf("HTTP %d%s", res.StatusCode, auth)}
}

// checkFeed issues a HEAD against the feed URL and prints only the host.
// Some trackers reject HEAD; that is reported verbatim, not as failure
// of the URL itself.
func checkFeed(client *http.Client, feedURL string) result {
	host := feedURL
	if u, err := url.Parse(feedURL); err == nil && u.Host != "" {
		host = u.Host
	}
	req, err := http.NewRequest(http.MethodHead, feedURL, nil)
	if err != nil {
		return result{"rss:" + host, false, err.Error()}
	}
	res, err := client.Do(req)
	if err != nil {
		return result{"rss:" + host, false, err.Error()}
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusMethodNotAllowed || res.StatusCode == http.StatusNotImplemented {
		return checkFeedGet(client, host, feedURL)
	}
	return result{"rss:" + host, res.StatusCode < 500, fmt.Sprintf("HTTP %d", res.StatusCode)}
}

// checkFeedGet retries with a ranged GET for servers without HEAD support,
// discarding the body after the first bytes.
func checkFeedGet(client *http.Client, host, feedURL string) result {
	req, err := http.NewRequest(http.MethodGet, feedURL, nil)
	if err != nil {
		return result{"rss:" + host, false, err.Error()}
	}
	req.Header.Set("Range", "bytes=0-0")
	res, err := client.Do(req)
	if err != nil {
		return result{"rss:" + host, false, err.Error()}
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	ok := res.StatusCode < 500
	return result{"rss:" + host, ok, fmt.Sprintf("HTTP %d (via GET)", res.StatusCode)}
}
