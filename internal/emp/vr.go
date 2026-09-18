package emp

import (
	"regexp"
	"strings"

	"wankarr/internal/store"
)

var wordRe = regexp.MustCompile(`[a-z0-9]+`)

// vrTokens are whole-word signals. Short tokens match on token equality
// only, so "cover" never matches "vr" and "2160p" never matches "180".
var vrTokens = map[string]bool{
	"180": true, "360": true,
	"oculus": true, "vive": true, "rift": true, "quest": true,
	"gear": true, "gearvr": true, "psvr": true, "playstation": true,
	"samsung": true, "cardboard": true, "daydream": true, "pimax": true,
	"passthrough": true,
}

// notVRPhrases are title phrases that contain VR tokens but describe
// flat content: VR-to-flat conversions ("VR to Normal"), 2D versions,
// and explicit non-VR markers. Checked first so "VR" in these titles
// cannot pass the signal test below.
var notVRPhrases = []string{
	"vr to normal", "vr2normal", "vr to flat", "vr to 2d",
	"non vr", "nonvr", "not vr", "2d version", "flat version",
}

// IsVR reports whether an Emp item looks like VR content, from title and
// tags. Emp has no VR category (VR uploads land in arbitrary single
// categories), so keyword search cannot isolate VR — this local filter
// is what makes search results usable.
func IsVR(it store.Item) bool {
	lower := strings.ToLower(it.Title + " " + strings.Join(it.Tags, " "))
	for _, p := range notVRPhrases {
		if strings.Contains(lower, p) {
			return false
		}
	}
	toks := map[string]bool{}
	for _, w := range wordRe.FindAllString(strings.ToLower(it.Title+" "+strings.Join(it.Tags, " ")), -1) {
		toks[w] = true
	}
	if toks["vr"] {
		return true
	}
	if toks["virtual"] && toks["reality"] {
		return true
	}
	for s := range vrTokens {
		if toks[s] {
			return true
		}
	}
	return false
}
