// Package match cross-matches Emp variant groups against the XBVR
// wishlist at ingest time, so wanted scenes surface themselves instead
// of waiting to be stumbled upon while scrolling.
package match

import (
	"regexp"
	"strings"

	"wankarr/internal/emp"
	"wankarr/internal/xbvr"
)

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// Result is one candidate pairing of an Emp group with a wanted scene.
type Result struct {
	GroupKey string
	Scene    xbvr.WantedScene
	Score    float64 // 0..1, title-token overlap plus studio bonus
}

// normalize folds a title for comparison: lowercase, punctuation to spaces.
func normalize(s string) []string {
	s = strings.ToLower(s)
	s = nonWord.ReplaceAllString(s, " ")
	return strings.Fields(s)
}

// stopwords are tokens too generic to count toward a title match.
var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "in": true, "on": true, "of": true,
	"and": true, "vr": true, "3d": true, "180": true, "360": true,
}

// Score rates an Emp group key against a wanted scene. All significant
// tokens of the XBVR title must appear in the group key for a nonzero
// score; a studio/site token match adds a bonus.
func Score(groupKey string, w xbvr.WantedScene) float64 {
	return ScoreTitle(groupKey, w.Title, w.Site, w.Studio)
}

// ScoreTitle rates a group key against a bare title plus site/studio
// names: the same overlap as Score for library scenes, which are not
// wishlist entries.
func ScoreTitle(groupKey, title, site, studio string) float64 {
	titleToks := significant(normalize(title))
	if len(titleToks) == 0 {
		return 0
	}
	keySet := map[string]bool{}
	for _, t := range normalize(groupKey) {
		keySet[t] = true
	}
	hit := 0
	for _, t := range titleToks {
		if keySet[t] {
			hit++
		}
	}
	if hit == 0 {
		return 0
	}
	score := float64(hit) / float64(len(titleToks))
	if studioHitName(groupKey, site, studio) {
		score += 0.2
	}
	if score > 1 {
		score = 1
	}
	return score
}

// significant drops generic tokens (stopwords) and bare resolution
// tokens: a "4K" in the XBVR title must not demand a "4k" in the group
// key, which no longer carries resolution signals at all.
func significant(toks []string) []string {
	var out []string
	for _, t := range toks {
		if !stopwords[t] && !emp.ResToken.MatchString(t) {
			out = append(out, t)
		}
	}
	return out
}

// studioHit reports whether the studio/site name appears in the group key
// or the Emp title carries the studio as a "Name - ..." prefix.
func studioHit(groupKey string, w xbvr.WantedScene) bool {
	return studioHitName(groupKey, w.Site, w.Studio)
}

// studioHitName is studioHit over bare site/studio names.
func studioHitName(groupKey, site, studio string) bool {
	key := strings.ToLower(groupKey)
	for _, name := range []string{site, studio} {
		n := strings.ToLower(strings.TrimSpace(name))
		if n == "" {
			continue
		}
		compact := nonWord.ReplaceAllString(n, "")
		if compact != "" && strings.Contains(nonWord.ReplaceAllString(key, ""), compact) {
			return true
		}
	}
	return false
}

// Library is a precomputed snapshot of owned scenes for repeated group
// scoring. Scoring every group against every owned title from scratch
// on each view is O(groups × library) full tokenizations; the index
// pays tokenization once per snapshot refresh and reuses it.
type Library struct {
	toks    [][]string
	sites   []string
	heights []int
	titles  []string
}

// IndexLibrary precomputes the comparison tokens of owned scenes.
// Titles with no significant tokens are dropped (they can never hit).
func IndexLibrary(scenes []xbvr.OwnedScene) Library {
	var l Library
	for _, s := range scenes {
		toks := significant(normalize(s.Title))
		if len(toks) == 0 {
			continue
		}
		site := strings.ToLower(strings.TrimSpace(s.Site))
		l.toks = append(l.toks, toks)
		l.sites = append(l.sites, nonWord.ReplaceAllString(site, ""))
		l.heights = append(l.heights, s.BestHeight)
		l.titles = append(l.titles, s.Title)
	}
	return l
}

// Best returns the best local height and scene title among owned scenes
// scoring at least threshold against the group key — the same arithmetic
// as ScoreTitle, with the key tokenized once. Ties keep the first scene,
// mirroring the inline loop it replaces. The title identifies the match
// so In-library chips are verifiable, not just claims.
func (l Library) Best(groupKey string, threshold float64) (height int, title string, ok bool) {
	keySet := map[string]bool{}
	for _, t := range normalize(groupKey) {
		keySet[t] = true
	}
	compactKey := nonWord.ReplaceAllString(strings.ToLower(groupKey), "")
	bestScore := 0.0
	for i, toks := range l.toks {
		hit := 0
		for _, t := range toks {
			if keySet[t] {
				hit++
			}
		}
		if hit == 0 {
			continue
		}
		score := float64(hit) / float64(len(toks))
		if c := l.sites[i]; c != "" && strings.Contains(compactKey, c) {
			score += 0.2
		}
		if score > 1 {
			score = 1
		}
		if score >= threshold && score > bestScore {
			height, title, bestScore, ok = l.heights[i], l.titles[i], score, true
		}
	}
	return height, title, ok
}

// AgainstWishlist scores every group against every wanted scene and
// returns matches at or above threshold.
func AgainstWishlist(groups map[string][]emp.Variant, wishlist []xbvr.WantedScene, threshold float64) []Result {
	var out []Result
	for key := range groups {
		for _, w := range wishlist {
			if s := Score(key, w); s >= threshold {
				out = append(out, Result{GroupKey: key, Scene: w, Score: s})
			}
		}
	}
	return out
}
