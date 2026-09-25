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
	if h, title, ok := lib.Best("[virtual papi] sfizydyd (next door peep)", nil, 0.6); !ok || h != 720 || title != "SfizyDyd (Next Door Peep)" {
		t.Errorf("best = %d, %q, %v; want 720, the scene title, true", h, title, ok)
	}
	if _, _, ok := lib.Best("some other scene entirely", nil, 0.6); ok {
		t.Error("unrelated key matched, want false")
	}
	// Near-miss overlap below threshold: single shared token.
	if _, _, ok := lib.Best("sfizydyd", nil, 0.6); ok {
		t.Error("below-threshold overlap matched, want false")
	}
}

// Reported false positive: a one-word library title ("Slut") scored
// 1.0 against every group containing that word. Single-token titles
// carry no identifying power and must never claim a match.
func TestSingleTokenTitleNeverMatches(t *testing.T) {
	lib := IndexLibrary([]xbvr.OwnedScene{
		{SceneID: "x-1", Title: "Slut", Site: "Virtual Papi", BestHeight: 2880},
	})
	if _, _, ok := lib.Best("[virtual papi] maya rose (french little slut)", nil, 0.6); ok {
		t.Error("single-token owned title matched, want false")
	}
	w := xbvr.WantedScene{SceneID: "x-1", Title: "Slut", Site: "Virtual Papi"}
	if s := Score("[virtual papi] maya rose (french little slut)", w); s != 0 {
		t.Errorf("single-token wanted title scored %v, want 0", s)
	}
}

// Byte-equality rescue: a Jaccard-killed title match is reinstated when
// every significant owned token is in the key and a variant is the same
// bytes as an owned file (e.g. a compilation under a performer-heavy
// key). Same name plus same bytes is near-certain.
func TestBestRescuesByteEqualCompilation(t *testing.T) {
	const gb = int64(1) << 30
	ownedSize := 35 * gb
	lib := IndexLibrary([]xbvr.OwnedScene{
		{SceneID: "fp-c", Title: "Best Curvy Cowgirl Adventures Vol.1", Site: "FuckPassVR", BestHeight: 1920, Sizes: []int64{ownedSize}},
	})
	key := emp.GroupKey("[FuckPassVR] Payton Preslee, Lily Starfire, Lauren Phillips, Skylar Snow, Lexi Luv, Sarah Arabic, Jennifer Mendez (Best Curvy Cowgirl Adventures Vol.1) [180°, 8k, 4096, Oculus Rift / Vive]")
	// Title alone: far below the Jaccard floor.
	if _, _, ok := lib.Best(key, nil, 0.6); ok {
		t.Fatal("title-only match, want false")
	}
	h, title, ok := lib.Best(key, []int64{ownedSize}, 0.6)
	if !ok || h != 1920 || title != "Best Curvy Cowgirl Adventures Vol.1" {
		t.Fatalf("rescued = %d, %q, %v; want 1920, the compilation, true", h, title, ok)
	}
	// 1% drift still rescues; 10% does not.
	if _, _, ok := lib.Best(key, []int64{ownedSize * 101 / 100}, 0.6); !ok {
		t.Error("1% size drift: want rescue")
	}
	if _, _, ok := lib.Best(key, []int64{ownedSize * 11 / 10}, 0.6); ok {
		t.Error("10% size drift: want no rescue")
	}
	// Unknown sizes never rescue.
	if _, _, ok := lib.Best(key, []int64{0}, 0.6); ok {
		t.Error("zero variant size: want no rescue")
	}
}

// Rescue needs perfect recall: a missing title token plus a byte-equal
// variant is still not a match.
func TestRescueNeedsPerfectRecall(t *testing.T) {
	const gb = int64(1) << 30
	size := 10 * gb
	lib := IndexLibrary([]xbvr.OwnedScene{
		{SceneID: "x", Title: "Closing A Deal Remastered", Site: "SLR", BestHeight: 1024, Sizes: []int64{size}},
	})
	key := emp.GroupKey("SLROriginals - Earn the Deal - Melissa Stratton (2026.09.23) (Oculus 4K)")
	if _, _, ok := lib.Best(key, []int64{size}, 0.6); ok {
		t.Error("imperfect recall with size hit: want no rescue")
	}
}

// Rescue fills gaps only: a title-scored match is never overridden.
func TestRescueNeverOverridesScored(t *testing.T) {
	const gb = int64(1) << 30
	size := 10 * gb
	lib := IndexLibrary([]xbvr.OwnedScene{
		{SceneID: "scored", Title: "Rainy City Rendezvous", Site: "FuckPassVR", BestHeight: 1920},
		{SceneID: "rescue", Title: "Rainy City", Site: "Other", BestHeight: 720, Sizes: []int64{size}},
	})
	key := emp.GroupKey("FuckPassVR - Rainy City Rendezvous - Mia James (2026.08.28) (Oculus 8K, UHD)")
	h, title, ok := lib.Best(key, []int64{size}, 0.6)
	if !ok || h != 1920 || title != "Rainy City Rendezvous" {
		t.Fatalf("got %d, %q, %v; want the scored match", h, title, ok)
	}
}

// Reported false positives: high recall against long keys via performer
// names, generic words, series templates, and site-bonus pushes — all
// sharing little with the key itself. The Jaccard floor rejects them.
func TestReportedFalsePositivesRejected(t *testing.T) {
	cases := []struct {
		name       string
		groupTitle string
		owned      xbvr.OwnedScene
	}{
		// Performer collision: compilation key holds the performer's name.
		{"performer", "[FuckPassVR] Payton Preslee, Lily Starfire, Lauren Phillips, Skylar Snow, Lexi Luv, Sarah Arabic, Jennifer Mendez (Best Curvy Cowgirl Adventures Vol.1) [180°, 8k, 4096, Oculus Rift / Vive]",
			xbvr.OwnedScene{Title: "Lauren Phillips : Lauren Loves Anal", Site: "FuckPassVR"}},
		// Same performer, different scene.
		{"same performer", "PassthroughVR - Dream Cumming True - Lauren Phillips (2026.04.03) (Oculus 8K)",
			xbvr.OwnedScene{Title: "Lauren Phillips : Lauren Loves Anal", Site: "PassthroughVR"}},
		// Generic-word overlap at 2/3 recall.
		{"generic words", "[Virtual Papi] Maya Rose (French Little Slut) [VR, 60 FPS, 180°, 8K, 3840p] [Oculus Rift / Vive]",
			xbvr.OwnedScene{Title: "Little Slut Diaries", Site: "Virtual Papi"}},
		// Perfect recall on generic words alone.
		{"generic perfect recall", "[VRXClouds] Lesly Clap (She Shows Off Her Ass, and He Fucks Her Twice in a Row) [VR, 60 FPS, 180°, 8K, 3840p] [Oculus Rift / Vive]",
			xbvr.OwnedScene{Title: "On Her Ass", Site: "VRXClouds"}},
		// Series confusion: different parody, same studio template.
		{"series", "VRCosplayX - Buffy: Faith A XXX Parody - Serena Hill (2026.09.24) (Oculus 8K)",
			xbvr.OwnedScene{Title: "Tron A XXX Parody", Site: "VRCosplayX"}},
		// Template similarity at 0.8 recall.
		{"template", "ARPorn - Scratch Me If You Can - Aleksa Mink (2026.09.25) (Oculus 8K)",
			xbvr.OwnedScene{Title: "Fuck Me If You Can", Site: "ARPorn"}},
		// Site bonus pushing sub-threshold recall over the line.
		{"bonus push", "SLROriginals - Earn the Deal - Melissa Stratton (2026.09.23) (Oculus 4K)",
			xbvr.OwnedScene{Title: "Closing A Deal", Site: "SLROriginals"}},
	}
	for _, tc := range cases {
		key := emp.GroupKey(tc.groupTitle)
		if s := ScoreTitle(key, tc.owned.Title, tc.owned.Site, ""); s != 0 {
			t.Errorf("%s: ScoreTitle = %v, want 0", tc.name, s)
		}
		lib := IndexLibrary([]xbvr.OwnedScene{tc.owned})
		if _, _, ok := lib.Best(key, nil, 0.6); ok {
			t.Errorf("%s: index matched, want false", tc.name)
		}
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
