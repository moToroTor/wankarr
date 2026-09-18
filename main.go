// Command wankarr is the Emp companion sidecar for XBVR.
//
// It keeps a local index of Emp uploads (RSS feeds + Jackett searches),
// matches them against the XBVR library and wishlist, and sends chosen
// torrents to Transmission. See README.md for setup.
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"wankarr/internal/config"
	"wankarr/internal/doctor"
	"wankarr/internal/emp"
	"wankarr/internal/jackett"
	"wankarr/internal/match"
	"wankarr/internal/store"
	"wankarr/internal/torrent"
	"wankarr/internal/xbvr"
)

//go:embed web/index.html web/app.js web/style.css
var webFiles embed.FS

func main() {
	cfg, err := config.Load(config.DefaultDotenvPath("."))
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		os.Exit(doctor.Run(cfg))
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer db.Close()

	xc := xbvr.NewClient(cfg.XBVRURL)
	httpClient := &http.Client{Timeout: 60 * time.Second}

	// Initial reconcile + poll at startup, then on the configured
	// interval. RSS is the polite standing index: conditional GETs,
	// a few requests a day.
	go func() {
		matchAll(db, xc)
		poll(cfg, db, xc, httpClient)
		for range time.Tick(cfg.PollInterval) {
			poll(cfg, db, xc, httpClient)
			matchAll(db, xc)
		}
	}()

	// Completion watch: wishlist grabs are linked into their XBVR scene
	// once Transmission finishes downloading them.
	go func() {
		checkCompletions(cfg, db)
		for range time.Tick(completionInterval) {
			checkCompletions(cfg, db)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "ok items=%d\n", mustCount(db))
	})
	webRoot, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatalf("web assets: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(webRoot)))
	mux.HandleFunc("/api/groups", func(w http.ResponseWriter, r *http.Request) {
		serveGroups(db, cfg, xc, w, r)
	})
	mux.HandleFunc("/api/matches", func(w http.ResponseWriter, r *http.Request) {
		serveMatches(db, w, r)
	})
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		serveSearch(cfg, db, xc, w, r)
	})
	mux.HandleFunc("/api/send", func(w http.ResponseWriter, r *http.Request) {
		serveSend(cfg, db, w, r)
	})
	mux.HandleFunc("/api/wishlist", func(w http.ResponseWriter, r *http.Request) {
		serveWishlist(db, cfg, xc, w, r)
	})
	mux.HandleFunc("/api/rematch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		go matchAll(db, xc)
		writeJSON(w, map[string]any{"started": true})
	})

	addr := fmt.Sprintf("127.0.0.1:%d", cfg.HTTPPort)
	fmt.Printf("wankarr: serving on http://%s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

// poll fetches each configured feed, stores new items, and cross-matches
// fresh groups against the XBVR wishlist.
func poll(cfg *config.Config, db *store.DB, xc *xbvr.Client, hc *http.Client) {
	for _, feedURL := range cfg.RSSFeeds {
		etagKey, modKey := "etag:"+feedURL, "mod:"+feedURL
		raw, etag, mod, hit, err := emp.FetchFeed(hc, feedURL, db.GetKV(etagKey), db.GetKV(modKey))
		if err != nil {
			log.Printf("poll %s: %v", redactURL(feedURL), err)
			continue
		}
		if err := db.SetKV(etagKey, etag); err != nil {
			log.Printf("poll state: %v", err)
		}
		if err := db.SetKV(modKey, mod); err != nil {
			log.Printf("poll state: %v", err)
		}
		if hit {
			continue
		}
		items, err := emp.ParseFeed(raw, "rss", time.Now().UTC())
		if err != nil {
			log.Printf("poll %s: %v", redactURL(feedURL), err)
			continue
		}
		var fresh []store.Item
		for _, it := range items {
			isNew, err := db.UpsertItem(it)
			if err != nil {
				log.Printf("store: %v", err)
				continue
			}
			if isNew {
				fresh = append(fresh, it)
			}
		}
		log.Printf("poll %s: %d items, %d new", redactURL(feedURL), len(items), len(fresh))
		if len(fresh) > 0 {
			matchFresh(db, xc, fresh)
		}
	}
}

// matchFresh groups fresh items and records wishlist pairings.
func matchFresh(db *store.DB, xc *xbvr.Client, fresh []store.Item) {
	wishlist, err := xc.ListWishlist()
	if err != nil {
		log.Printf("wishlist: %v", err)
		return
	}
	groups := emp.Group(fresh)
	keepVR(groups)
	results := match.AgainstWishlist(groups, wishlist, 0.6)
	saveResults(db, results)
}

// matchAll reconciles the whole local index against the current wishlist.
// Fresh-item matching alone misses pairings when the wishlist entry is
// added after the Emp rows were stored, so this runs at startup, after
// every poll, and on demand via /api/rematch.
func matchAll(db *store.DB, xc *xbvr.Client) {
	wishlist, err := xc.ListWishlist()
	if err != nil {
		log.Printf("rematch: wishlist: %v", err)
		return
	}
	items, err := db.Get(0)
	if err != nil {
		log.Printf("rematch: store: %v", err)
		return
	}
	groups := emp.Group(items)
	keepVR(groups)
	results := match.AgainstWishlist(groups, wishlist, 0.6)
	saveResults(db, results)
}

func keepVR(groups map[string][]emp.Variant) {
	for key, vs := range groups {
		if !anyVR(vs) {
			delete(groups, key)
		}
	}
}

func saveResults(db *store.DB, results []match.Result) {
	var ms []store.Match
	for _, r := range results {
		ms = append(ms, store.Match{GroupKey: r.GroupKey, SceneID: r.Scene.SceneID, Score: r.Score})
	}
	if err := db.SaveMatches(ms); err != nil {
		log.Printf("matches: %v", err)
		return
	}
	log.Printf("matches: %d wishlist pairings", len(ms))
}

// redactURL strips query strings (feed URLs carry private tokens).
func redactURL(u string) string {
	for i, c := range u {
		if c == '?' {
			return u[:i] + "?…"
		}
	}
	return u
}

type variantView struct {
	GroupID     string `json:"group_id"`
	Title       string `json:"title"`
	Height      int    `json:"height"`
	Resolution  string `json:"resolution"`
	Codec       string `json:"codec,omitempty"`
	HBR         bool   `json:"hbr"`
	SizeBytes   int64  `json:"size_bytes"`
	Seeders     int    `json:"seeders"`
	Freeleech   bool   `json:"freeleech"`
	Source      string `json:"source"`
	PubDate     string `json:"pub_date"`
	Pick        bool   `json:"pick"`
	PremiumNote string `json:"premium_note,omitempty"`
}

type groupView struct {
	Key         string        `json:"key"`
	Title       string        `json:"title"`
	Cover       string        `json:"cover,omitempty"`
	Variants    []variantView `json:"variants"`
	WantedScene string        `json:"wanted_scene,omitempty"`
	WantedScore float64       `json:"wanted_score,omitempty"`
}

func serveGroups(db *store.DB, cfg *config.Config, xc *xbvr.Client, w http.ResponseWriter, r *http.Request) {
	items, err := db.Get(500)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	// Wishlist enrichment is best-effort: XBVR down means no wanted flags,
	// never a failed page.
	var wishlist []xbvr.WantedScene
	if ws, err := xc.ListWishlist(); err == nil {
		wishlist = ws
	} else {
		log.Printf("groups: wishlist unavailable: %v", err)
	}
	vrOnly := r.URL.Query().Get("vr") != "0"
	q := r.URL.Query().Get("q")
	items = filterQuery(items, q)
	writeJSON(w, buildGroupViews(emp.Profile{}, cfg, wishlist, items, vrOnly))
}

// buildGroupViews clusters items into enriched scene groups. Wishlist
// pairing drives the wanted flag and cover preference.
func buildGroupViews(profile emp.Profile, cfg *config.Config, wishlist []xbvr.WantedScene, items []store.Item, vrOnly bool) []groupView {
	out := []groupView{}
	for key, vs := range emp.Group(items) {
		if vrOnly && !anyVR(vs) {
			continue
		}
		g := groupView{Key: key, Title: vs[0].Item.Title}
		pick := profile.Pick(vs)
		var biggestPlain int64
		for _, v := range vs {
			if !v.HBR && v.Item.SizeBytes > biggestPlain {
				biggestPlain = v.Item.SizeBytes
			}
		}
		for i, v := range vs {
			vv := variantView{
				GroupID: v.Item.GroupID, Title: v.Item.Title,
				Height: v.Height, Resolution: v.Resolution, Codec: v.Codec, HBR: v.HBR,
				SizeBytes: v.Item.SizeBytes, Seeders: v.Item.Seeders, Freeleech: v.Item.Freeleech,
				Source:  v.Item.Source,
				PubDate: v.Item.PubDate.Format(time.RFC3339),
				Pick:    i == pick,
			}
			if v.HBR && biggestPlain > 0 && v.Item.SizeBytes > biggestPlain {
				vv.PremiumNote = fmt.Sprintf("+%s over largest non-HBR", humanBytes(v.Item.SizeBytes-biggestPlain))
			}
			g.Variants = append(g.Variants, vv)
		}
		var wantCover string
		for _, want := range wishlist {
			if s := match.Score(key, want); s >= 0.6 && s > g.WantedScore {
				g.WantedScene, g.WantedScore = want.Title, s
				wantCover = want.CoverURL
			}
		}
		// Prefer XBVR's cover on matched groups (it is the canonical
		// artwork for the scene); otherwise the newest Emp poster.
		if wantCover != "" {
			g.Cover = absolutizeURL(cfg.XBVRURL, wantCover)
		} else {
			for _, v := range vs {
				if v.Item.CoverURL != "" {
					g.Cover = v.Item.CoverURL
					break
				}
			}
		}
		out = append(out, g)
	}
	return out
}

// filterQuery keeps items matching every token of q (case-insensitive,
// title and tags). Empty q keeps everything.
func filterQuery(items []store.Item, q string) []store.Item {
	toks := strings.Fields(strings.ToLower(q))
	if len(toks) == 0 {
		return items
	}
	var out []store.Item
	for _, it := range items {
		hay := strings.ToLower(it.Title + " " + strings.Join(it.Tags, " "))
		keep := true
		for _, t := range toks {
			if !strings.Contains(hay, t) {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, it)
		}
	}
	return out
}

// anyVR reports whether at least one variant in the group looks like VR.
func anyVR(vs []emp.Variant) bool {
	for _, v := range vs {
		if emp.IsVR(v.Item) {
			return true
		}
	}
	return false
}

// absolutizeURL resolves root-relative XBVR asset paths against the
// instance base URL; absolute URLs pass through untouched.
func absolutizeURL(base, u string) string {
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(u, "/")
}

// humanBytes renders byte counts for premium notes.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), units[exp])
}

// serveSearch runs one explicit Jackett/Torznab query, stores results,
// and cross-matches them against the wishlist. It is the only handler
// that generates Emp-side traffic on demand.
func serveSearch(cfg *config.Config, db *store.DB, xc *xbvr.Client, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "missing q", http.StatusBadRequest)
		return
	}
	if cfg.JackettURL == "" || cfg.JackettAPIKey == "" {
		http.Error(w, "jackett not configured", http.StatusServiceUnavailable)
		return
	}
	// One query per configured indexer path; group IDs dedupe the rest.
	seen := map[string]bool{}
	var fresh []store.Item
	results, failures := 0, 0
	for _, path := range cfg.JackettTorznabPaths {
		jc := jackett.NewClient(cfg.JackettURL, cfg.JackettAPIKey, path)
		items, err := jc.Search(q, emp.GroupIDFromURL)
		if err != nil {
			log.Printf("search %s: %v", path, err)
			failures++
			continue
		}
		results += len(items)
		for _, it := range items {
			if seen[it.GroupID] {
				continue
			}
			seen[it.GroupID] = true
			isNew, err := db.UpsertItem(it)
			if err != nil {
				log.Printf("store: %v", err)
				continue
			}
			if isNew {
				fresh = append(fresh, it)
			}
		}
	}
	if failures == len(cfg.JackettTorznabPaths) {
		http.Error(w, "search failed", http.StatusBadGateway)
		return
	}
	if len(fresh) > 0 {
		matchFresh(db, xc, fresh)
	}
	writeJSON(w, map[string]any{"results": results, "new": len(fresh)})
}

// serveSend queues one indexed torrent in Transmission. The request names
// a group ID only; the credential-bearing enclosure URL never leaves the
// server, and only the group ID is logged.
func serveSend(cfg *config.Config, db *store.DB, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		GroupID string `json:"group_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.GroupID == "" {
		http.Error(w, "missing group_id", http.StatusBadRequest)
		return
	}
	dl, err := db.EnclosureURL(req.GroupID)
	if err != nil || dl == "" {
		http.Error(w, "unknown group", http.StatusNotFound)
		return
	}
	tc := torrent.NewTransmissionWithDir(cfg.TransmissionURL, cfg.TransmissionUser, cfg.TransmissionPass, cfg.TransmissionDownloadDir)
	name, tid, err := tc.AddURL(dl)
	if err != nil {
		log.Printf("send group %s: %v", req.GroupID, err)
		http.Error(w, "download client failed", http.StatusBadGateway)
		return
	}
	log.Printf("send group %s: queued %q", req.GroupID, name)
	resp := map[string]any{"queued": name}
	// Wishlist grabs get completion tracking: once Transmission
	// finishes, the file is linked into its XBVR scene. Plain
	// (non-wishlist) sends stay fire-and-forget.
	if sceneID, sceneTitle := wishlistSceneForGroup(db, xbvr.NewClient(cfg.XBVRURL), req.GroupID); sceneID != "" {
		if _, err := db.AddPending(tid, name, req.GroupID, sceneID, sceneTitle); err != nil {
			log.Printf("send group %s: pending track: %v", req.GroupID, err)
		} else {
			log.Printf("send group %s: tracking completion for scene %s", req.GroupID, sceneID)
			resp["wishlist_scene"] = sceneID
		}
	}
	writeJSON(w, resp)
}

// wishlistSceneForGroup returns the best wishlist scene for a group ID
// (match score >= 0.6), or "" when the group is not a wishlist grab or
// XBVR is unreachable. Best-effort: never fails the send.
func wishlistSceneForGroup(db *store.DB, xc *xbvr.Client, groupID string) (string, string) {
	it, err := db.GetItem(groupID)
	if err != nil {
		return "", ""
	}
	wishlist, err := xc.ListWishlist()
	if err != nil {
		log.Printf("send group %s: wishlist unavailable: %v", groupID, err)
		return "", ""
	}
	key := emp.GroupKey(it.Title)
	bestID, bestTitle, best := "", "", 0.0
	for _, want := range wishlist {
		if s := match.Score(key, want); s >= 0.6 && s > best {
			bestID, bestTitle, best = want.SceneID, want.Title, s
		}
	}
	return bestID, bestTitle
}

// Completion polling for wishlist grabs. Runs every few minutes:
//
//	downloading --torrent-get--> finished? --rescan--> downloaded
//	downloaded  --files/list--> found? --match if needed--> linked
//
// A torrent Transmission no longer knows is marked lost. Rescans are
// capped (XBVR scans are expensive) with a cooldown between retries.
const (
	completionInterval = 5 * time.Minute
	maxLinkRescans     = 3
	rescanCooldown     = 15 * time.Minute
)

func checkCompletions(cfg *config.Config, db *store.DB) {
	pend, err := db.ListActivePending()
	if err != nil {
		log.Printf("completions: %v", err)
		return
	}
	if len(pend) == 0 {
		return
	}
	tc := torrent.NewTransmissionWithDir(cfg.TransmissionURL, cfg.TransmissionUser, cfg.TransmissionPass, cfg.TransmissionDownloadDir)
	xc := xbvr.NewClient(cfg.XBVRURL)
	for _, p := range pend {
		switch p.Status {
		case "downloading":
			checkDownload(db, tc, xc, p)
		case "downloaded":
			checkLink(db, xc, p)
		default:
			log.Printf("completions: pending %d in unexpected state %q", p.ID, p.Status)
		}
	}
}

// checkDownload advances a pending row once its torrent finishes.
func checkDownload(db *store.DB, tc *torrent.Transmission, xc *xbvr.Client, p store.PendingLink) {
	st, err := tc.StatusOf(p.TransmissionID)
	if err != nil {
		if errors.Is(err, torrent.ErrTorrentNotFound) {
			log.Printf("completions: pending %d: torrent gone from client, giving up", p.ID)
			_ = db.SetPendingStatus(p.ID, "lost")
			return
		}
		log.Printf("completions: pending %d: status: %v", p.ID, err)
		return
	}
	if !st.Done() {
		return
	}
	if err := xc.Rescan(); err != nil {
		log.Printf("completions: pending %d: rescan: %v", p.ID, err)
		return
	}
	if err := db.NoteRescan(p.ID); err != nil {
		log.Printf("completions: pending %d: %v", p.ID, err)
		return
	}
	if err := db.SetPendingStatus(p.ID, "downloaded"); err != nil {
		log.Printf("completions: pending %d: %v", p.ID, err)
		return
	}
	log.Printf("completions: pending %d (%q): downloaded, rescan queued", p.ID, p.TorrentName)
}

// checkLink looks for the finished file in XBVR and links it to the
// wishlist scene when the rescan's auto-match did not.
func checkLink(db *store.DB, xc *xbvr.Client, p store.PendingLink) {
	f, found, err := xc.FindFile(p.TorrentName)
	if err != nil {
		log.Printf("completions: pending %d: find file: %v", p.ID, err)
		return
	}
	if !found {
		if p.Rescans < maxLinkRescans && time.Since(p.RescanAt) > rescanCooldown {
			if err := xc.Rescan(); err != nil {
				log.Printf("completions: pending %d: rescan retry: %v", p.ID, err)
				return
			}
			_ = db.NoteRescan(p.ID)
			log.Printf("completions: pending %d (%q): still unseen, rescan %d queued",
				p.ID, p.TorrentName, p.Rescans+1)
		}
		return
	}
	if f.SceneID == 0 {
		if err := xc.MatchFile(p.SceneID, f.ID); err != nil {
			log.Printf("completions: pending %d: match file %d: %v", p.ID, f.ID, err)
			return
		}
		log.Printf("completions: pending %d: linked file %d to scene %s", p.ID, f.ID, p.SceneID)
	} else {
		log.Printf("completions: pending %d: file %d already linked, done", p.ID, f.ID)
	}
	_ = db.SetPendingStatus(p.ID, "linked")
}

// serveWishlist renders the XBVR wishlist itself — artwork and details
// from XBVR, no Emp traffic — with already-known Emp groups inline.
// Each row's Search button hits /api/search, which queries Jackett once
// and caches the results locally (a refresh when groups are known).
func serveWishlist(db *store.DB, cfg *config.Config, xc *xbvr.Client, w http.ResponseWriter, r *http.Request) {
	type wantedView struct {
		SceneID    string      `json:"scene_id"`
		Title      string      `json:"title"`
		Site       string      `json:"site"`
		Cover      string      `json:"cover,omitempty"`
		Performers []string    `json:"performers,omitempty"`
		Groups     []groupView `json:"groups"`
	}
	wishlist, err := xc.ListWishlist()
	if err != nil {
		log.Printf("wishlist view: %v", err)
		http.Error(w, "xbvr unavailable", http.StatusBadGateway)
		return
	}
	items, err := db.Get(0)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	groups := buildGroupViews(emp.Profile{}, cfg, wishlist, items, true)
	out := []wantedView{}
	for _, want := range wishlist {
		wv := wantedView{
			SceneID: want.SceneID, Title: want.Title, Site: want.Site,
			Cover:      absolutizeURL(cfg.XBVRURL, want.CoverURL),
			Performers: want.Performers,
			Groups:     []groupView{},
		}
		for _, g := range groups {
			if match.Score(g.Key, want) >= 0.6 {
				wv.Groups = append(wv.Groups, g)
			}
		}
		out = append(out, wv)
	}
	writeJSON(w, out)
}

func serveMatches(db *store.DB, w http.ResponseWriter, r *http.Request) {
	ms, err := db.ListMatches(200)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, ms)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func mustCount(db *store.DB) int {
	n, err := db.Count()
	if err != nil {
		return -1
	}
	return n
}
