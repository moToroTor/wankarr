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

// ResToken spots bare resolution tokens ("2K", "4096p", "UHD") on
// lowercased title tokens. They are variant signals, not scene identity:
// uploaders append them outside the variant parenthetical.
var ResToken = regexp.MustCompile(`^(\d{3,4}p|[24568]k|uhd|fhd|qhd|hd|sd)$`)

// GroupKey normalizes a title for clustering: the scene identity is the
// title minus the trailing variant parenthetical ("(Oculus 8K, UHD)")
// and minus bare resolution tokens ("2K", "4096p"). The key doubles as
// the Find-versions Jackett query, so a lingering "2K" would hide the
// 4K/8K uploads of the same scene.
func GroupKey(title string) string {
	key := strings.TrimSpace(title)
	for {
		next := trailingVariantRe.ReplaceAllString(key, "")
		next = strings.TrimSpace(next)
		if next == key {
			break
		}
		key = next
	}
	lower := strings.ToLower(key)
	var toks []string
	for _, t := range strings.Fields(lower) {
		if !ResToken.MatchString(strings.Trim(t, ",;:.!?")) {
			toks = append(toks, t)
		}
	}
	if len(toks) == 0 {
		return lower
	}
	return strings.Join(toks, " ")
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

// dateRe removes release-date parentheticals, which vary between uploads
// of the same scene ("(2026.09.07)") and would otherwise split groups.
var dateRe = regexp.MustCompile(`\(\d{4}[.-]\d{2}[.-]\d{2}\)`)

// mergeThreshold is the minimum token-set Jaccard similarity for fusing
// two title buckets. Uploaders reorder performer/scene tokens
// ("Site 899 - Scene - Performer" vs "Site 899 - Performer - Scene"),
// so exact keys split real scenes; 0.8 keeps "899" vs "900" apart.
const mergeThreshold = 0.8

// keyTokens returns the significant token set of a group key.
func keyTokens(key string) map[string]bool {
	toks := map[string]bool{}
	for _, t := range strings.Fields(dateRe.ReplaceAllString(key, " ")) {
		t = strings.Trim(t, "-_.,;:!?()[]")
		if t != "" {
			toks[t] = true
		}
	}
	return toks
}

// mergeSimilar fuses buckets whose token sets overlap at (or above)
// mergeThreshold, keeping the lexicographically smallest key for
// determinism. Requires ≥4 shared tokens so tiny titles can't merge on
// one shared word.
func mergeSimilar(groups map[string][]Variant) map[string][]Variant {
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	tokSets := map[string]map[string]bool{}
	for _, k := range keys {
		tokSets[k] = keyTokens(k)
	}
	parent := map[string]string{}
	for _, k := range keys {
		parent[k] = k
	}
	var find func(string) string
	find = func(x string) string {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			a, b := tokSets[keys[i]], tokSets[keys[j]]
			inter := 0
			for t := range a {
				if b[t] {
					inter++
				}
			}
			union := len(a) + len(b) - inter
			if inter >= 4 && union > 0 && float64(inter)/float64(union) >= mergeThreshold {
				ra, rb := find(keys[i]), find(keys[j])
				if ra != rb {
					if rb < ra {
						ra, rb = rb, ra
					}
					parent[rb] = ra
				}
			}
		}
	}
	out := map[string][]Variant{}
	for _, k := range keys {
		r := find(k)
		out[r] = append(out[r], groups[k]...)
	}
	return out
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
	groups = mergeSimilar(groups)
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
