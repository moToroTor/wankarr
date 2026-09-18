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
	titleToks := significant(normalize(w.Title))
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
	if studioHit(groupKey, w) {
		score += 0.2
	}
	if score > 1 {
		score = 1
	}
	return score
}

func significant(toks []string) []string {
	var out []string
	for _, t := range toks {
		if !stopwords[t] {
			out = append(out, t)
		}
	}
	return out
}

// studioHit reports whether the studio/site name appears in the group key
// or the Emp title carries the studio as a "Name - ..." prefix.
func studioHit(groupKey string, w xbvr.WantedScene) bool {
	key := strings.ToLower(groupKey)
	for _, name := range []string{w.Site, w.Studio} {
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
