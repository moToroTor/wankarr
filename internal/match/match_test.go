package match

import (
	"testing"

	"wankarr/internal/emp"
	"wankarr/internal/xbvr"
)

func TestScoreMatchesVariantGroup(t *testing.T) {
	key := emp.GroupKey("FuckPassVR - Rainy City Rendezvous - Mia James (2026.08.28) (Oculus 8K, UHD)")
	w := xbvr.WantedScene{SceneID: "fp-1", Title: "Rainy City Rendezvous", Site: "FuckPassVR"}
	if s := Score(key, w); s < 1 {
		t.Errorf("score = %v, want 1 (full title + studio)", s)
	}
}

// Resolution is not identity: a "4K" in the XBVR title must not demand
// a "4k" in the group key, which no longer carries resolution signals.
func TestScoreIgnoresResolution(t *testing.T) {
	key := emp.GroupKey("[Virtual Papi] SfizyDyd (Next Door Peep) 2K")
	w := xbvr.WantedScene{SceneID: "vp-1", Title: "SfizyDyd (Next Door Peep) 4K", Site: "Virtual Papi"}
	if s := Score(key, w); s < 1 {
		t.Errorf("score = %v, want 1 (resolution ignored both sides)", s)
	}
}

// ScoreTitle over the same fields agrees with Score exactly.
func TestScoreTitleMatchesScore(t *testing.T) {
	key := emp.GroupKey("FuckPassVR - Rainy City Rendezvous - Mia James (2026.08.28) (Oculus 8K, UHD)")
	w := xbvr.WantedScene{SceneID: "fp-1", Title: "Rainy City Rendezvous", Site: "FuckPassVR", Studio: "FuckPassVR"}
	if a, b := Score(key, w), ScoreTitle(key, w.Title, w.Site, w.Studio); a != b {
		t.Errorf("Score = %v, ScoreTitle = %v, want equal", a, b)
	}
}

// Best answers from the index: the matching scene's height, nothing
// for unrelated keys or below-threshold overlaps.
func TestLibraryBest(t *testing.T) {
	lib := IndexLibrary([]xbvr.OwnedScene{
		{SceneID: "vp-1", Title: "SfizyDyd (Next Door Peep)", Site: "Virtual Papi", BestHeight: 720},
		{SceneID: "other", Title: "Completely Different Words Here", Site: "Other Site", BestHeight: 1080},
	})
	if h, title, ok := lib.Best("[virtual papi] sfizydyd (next door peep)", 0.6); !ok || h != 720 || title != "SfizyDyd (Next Door Peep)" {
		t.Errorf("best = %d, %q, %v; want 720, the scene title, true", h, title, ok)
	}
	if _, _, ok := lib.Best("some other scene entirely", 0.6); ok {
		t.Error("unrelated key matched, want false")
	}
	// Near-miss overlap below threshold: single shared token.
	if _, _, ok := lib.Best("sfizydyd", 0.6); ok {
		t.Error("below-threshold overlap matched, want false")
	}
}

func TestScoreRejectsUnrelated(t *testing.T) {
	key := emp.GroupKey("VRCosplayX - The Legend of Vox Machina: Keyleth A XXX Parody - Gracey Snow (2026.09.17) (Oculus 8K)")
	w := xbvr.WantedScene{SceneID: "fp-1", Title: "Rainy City Rendezvous", Site: "FuckPassVR"}
	if s := Score(key, w); s != 0 {
		t.Errorf("score = %v, want 0", s)
	}
}

func TestAgainstWishlistThreshold(t *testing.T) {
	groups := map[string][]emp.Variant{
		emp.GroupKey("FuckPassVR - Rainy City Rendezvous - Mia James (2026.08.28) (Oculus 8K)"): {{Height: 1920}},
		emp.GroupKey("Some Other Scene (2026.01.01) (4K)"):                                      {{Height: 1024}},
	}
	wishlist := []xbvr.WantedScene{
		{SceneID: "fp-1", Title: "Rainy City Rendezvous", Site: "FuckPassVR"},
	}
	got := AgainstWishlist(groups, wishlist, 0.6)
	if len(got) != 1 || got[0].Scene.SceneID != "fp-1" {
		t.Fatalf("matches = %+v, want exactly the FuckPassVR pairing", got)
	}
}
