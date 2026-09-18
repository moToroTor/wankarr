package emp

import (
	"regexp"
	"sort"
	"strings"

	"wankarr/internal/store"
)

// Resolution signals, highest first. Emp uploaders tag VR variants with the
// horizontal pixel count (4096p/3840p = 8K class) and/or title tokens
// like "Oculus 8K"; both are consulted.
var resTags = []struct {
	tag    string
	height int // equirectangular frame height, for comparison with XBVR files
}{
	{"4096p", 2048}, {"3840p", 1920}, {"8k", 1920},
	{"3072p", 1536}, {"6k", 1536}, {"2700p", 1350}, {"1600p", 800},
	{"2048p", 1024}, {"1920p", 1024}, {"4k", 1024},
	{"1440p", 720},
}

var trailingVariantRe = regexp.MustCompile(`\s*\([^()]*\)\s*$`)

// hbrTitleRe spots high-bitrate signals in the title itself: uploaders
// mark HBR variants "(Oculus 8K, HBR)", "(Oculus 8K, HQ)", or with
// "high bitrate" in various spellings. This is a fallback for records
// (notably Jackett/Torznab ones) whose tag list carries no signal.
var hbrTitleRe = regexp.MustCompile(`\b(hbr|hq)\b|high[ ._\-]*bitrate`)

// Variant is one upload of a scene with derived quality signals.
type Variant struct {
	Item       store.Item
	Height     int  // derived frame height, 0 when unknown
	HBR        bool // high-bitrate signal in tags or title tokens
	Resolution string
	Codec      string // "h265", "h264", or "av1" when tagged, else ""
}

// GroupKey normalizes a title for clustering: the scene identity is the
// title minus the trailing variant parenthetical ("(Oculus 8K, UHD)").
func GroupKey(title string) string {
	key := strings.TrimSpace(title)
	for {
		next := trailingVariantRe.ReplaceAllString(key, "")
		next = strings.TrimSpace(next)
		if next == key {
			return strings.ToLower(next)
		}
		key = next
	}
}

// DeriveVariant extracts resolution and HBR signals from tags + title.
func DeriveVariant(it store.Item) Variant {
	v := Variant{Item: it}
	lowerTags := make(map[string]bool, len(it.Tags))
	for _, t := range it.Tags {
		lowerTags[strings.ToLower(t)] = true
	}
	v.HBR = lowerTags["high.bitrate"] || lowerTags["highbitrate"] || lowerTags["hbr"] || lowerTags["hq"]
	for _, t := range []string{"h.265", "h265", "hevc", "x265"} {
		if lowerTags[t] {
			v.Codec = "h265"
			break
		}
	}
	if v.Codec == "" {
		for _, t := range []string{"h.264", "h264", "avc", "x264"} {
			if lowerTags[t] {
				v.Codec = "h264"
				break
			}
		}
	}
	if v.Codec == "" {
		for _, t := range []string{"av1", "av01"} {
			if lowerTags[t] {
				v.Codec = "av1"
				break
			}
		}
	}
	title := strings.ToLower(it.Title)
	if !v.HBR && hbrTitleRe.MatchString(title) {
		v.HBR = true
	}
	for _, r := range resTags {
		if lowerTags[r.tag] {
			v.Height = r.height
			v.Resolution = r.tag
			break
		}
	}
	// Fall back to title tokens ("Oculus 8K", "Go 4K") when tags lack them.
	if v.Height == 0 {
		for _, r := range resTags {
			if strings.Contains(title, strings.TrimSuffix(r.tag, "p")) {
				v.Height = r.height
				v.Resolution = r.tag
				break
			}
		}
	}
	return v
}

// Group clusters items into scenes; variants sort best-first by height,
// non-HBR before HBR at equal height (same pixels, smaller file wins
// unless the profile says otherwise).
func Group(items []store.Item) map[string][]Variant {
	groups := map[string][]Variant{}
	for _, it := range items {
		if looksAccessory(it) {
			continue
		}
		key := GroupKey(it.Title)
		groups[key] = append(groups[key], DeriveVariant(it))
	}
	for key := range groups {
		vs := groups[key]
		sort.SliceStable(vs, func(i, j int) bool {
			if vs[i].Height != vs[j].Height {
				return vs[i].Height > vs[j].Height
			}
			if vs[i].HBR != vs[j].HBR {
				return !vs[i].HBR
			}
			return vs[i].Item.SizeBytes < vs[j].Item.SizeBytes
		})
		groups[key] = vs
	}
	return groups
}

// looksAccessory filters uploads that are not the scene itself:
// funscripts, subtitle packs, megapacks, picture sets.
func looksAccessory(it store.Item) bool {
	lower := strings.ToLower(it.Title + " " + strings.Join(it.Tags, " "))
	for _, sig := range []string{"funscript", "subtitle", "megapack", "picture", ".pics]", "siterip"} {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	// Tiny payloads (< 50 MiB video) are scripts/samples, not scenes,
	// unless the record carries no size at all (unknown, keep it).
	if it.SizeBytes > 0 && it.SizeBytes < 50<<20 {
		return true
	}
	return false
}

// Profile expresses the global quality preference.
type Profile struct {
	MaxHeight  int  // ignore variants above this height (0 = no cap)
	PreferHBR  bool // when true, HBR wins ties at equal height
	MinSeeders int  // Jackett-sourced records below this are skipped (0 = ignore)
}

// Pick returns the index of the profile-preferred variant.
func (p Profile) Pick(vs []Variant) int {
	best, bestIdx := -1, 0
	for i, v := range vs {
		if p.MaxHeight > 0 && v.Height > p.MaxHeight {
			continue
		}
		score := v.Height * 100
		if v.HBR && p.PreferHBR {
			score += 50
		}
		if v.HBR && !p.PreferHBR {
			score -= 1
		}
		if score > best {
			best, bestIdx = score, i
		}
	}
	return bestIdx
}
