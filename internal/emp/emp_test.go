package emp

import (
	"strings"
	"testing"
	"time"

	"wankarr/internal/store"
)

// Scrubbed fixture mirroring the real Emp RSS shape: same elements and
// tag vocabulary, fake host, no credentials.
const fixtureFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
<channel>
<title>All Torrents :: Emp</title>
<item>
<title><![CDATA[FuckPassVR - Rainy City Rendezvous - Mia James (2026.08.28) (Oculus 8K, UHD) ]]></title>
<link>https://example.invalid/torrents.php?id=1154515</link>
<category><![CDATA[Big Tits]]></category>
<pubDate>Fri, 18 Sep 2026 01:32:19 +0000</pubDate>
<tags><![CDATA[fuckpassvr.com mia.james 4096p pov 60.fps 180.degrees virtual.reality high.bitrate]]></tags>
<enclosure url="https://example.invalid/torrents.php?action=download&amp;id=1155049" length="162576" type="application/x-bittorrent"/>
<torrent xmlns="http://xmlns.ezrss.it/0.1/">
<fileName><![CDATA[scene-uhd.torrent]]></fileName>
<infoHash><![CDATA[AA11]]></infoHash>
<contentLength>25422398398</contentLength>
<contentLengthHR>23.68 GiB</contentLengthHR>
</torrent>
</item>
<item>
<title><![CDATA[FuckPassVR - Rainy City Rendezvous - Mia James (2026.08.28) (Oculus 8K) ]]></title>
<link>https://example.invalid/torrents.php?id=1154514</link>
<category><![CDATA[Big Tits]]></category>
<pubDate>Fri, 18 Sep 2026 01:32:10 +0000</pubDate>
<tags><![CDATA[fuckpassvr.com mia.james 3840p pov 60.fps 180.degrees virtual.reality]]></tags>
<enclosure url="https://example.invalid/torrents.php?action=download&amp;id=1155048" length="237100" type="application/x-bittorrent"/>
<torrent xmlns="http://xmlns.ezrss.it/0.1/">
<fileName><![CDATA[scene-8k.torrent]]></fileName>
<infoHash><![CDATA[BB22]]></infoHash>
<contentLength>9287056196</contentLength>
<contentLengthHR>8.65 GiB</contentLengthHR>
</torrent>
</item>
<item>
<title><![CDATA[FuckPassVR - Rainy City Rendezvous - Mia James (2026.08.28) (Oculus, Go 4K) ]]></title>
<link>https://example.invalid/torrents.php?id=1154513</link>
<category><![CDATA[Big Tits]]></category>
<pubDate>Fri, 18 Sep 2026 01:32:02 +0000</pubDate>
<tags><![CDATA[fuckpassvr.com mia.james 1920p pov 60.fps 180.degrees virtual.reality]]></tags>
<enclosure url="https://example.invalid/torrents.php?action=download&amp;id=1155047" length="190888" type="application/x-bittorrent"/>
<torrent xmlns="http://xmlns.ezrss.it/0.1/">
<fileName><![CDATA[scene-4k.torrent]]></fileName>
<infoHash><![CDATA[CC33]]></infoHash>
<contentLength>7469214197</contentLength>
<contentLengthHR>6.96 GiB</contentLengthHR>
</torrent>
</item>
<item>
<title><![CDATA[CzechVR 894 - Always Have Time for You - Sofia Vega (2026.08.19) (Oculus 6K) ]]></title>
<link>https://example.invalid/torrents.php?id=1154535</link>
<category><![CDATA[Big Tits]]></category>
<pubDate>Fri, 18 Sep 2026 06:55:57 +0000</pubDate>
<tags><![CDATA[czechvr.com sofia.vega oculus.quest.2 h.265 2700p pov 60.fps 180.degrees virtual.reality]]></tags>
<enclosure url="https://example.invalid/torrents.php?action=download&amp;id=1155046" length="190000" type="application/x-bittorrent"/>
<torrent xmlns="http://xmlns.ezrss.it/0.1/">
<fileName><![CDATA[scene-2700p.torrent]]></fileName>
<infoHash><![CDATA[DD44]]></infoHash>
<contentLength>10881611442</contentLength>
<contentLengthHR>10.13 GiB</contentLengthHR>
</torrent>
</item>
<item>
<title><![CDATA[Funscript for RealJamVR - Chill Out with Kali Roses (2026.09.08) ]]></title>
<link>https://example.invalid/torrents.php?id=1154511</link>
<category><![CDATA[Other]]></category>
<pubDate>Fri, 18 Sep 2026 01:26:01 +0000</pubDate>
<tags><![CDATA[funscript kali.roses other]]></tags>
<enclosure url="https://example.invalid/torrents.php?action=download&amp;id=1155045" length="828" type="application/x-bittorrent"/>
<fileName><![CDATA[scene.funscript.torrent]]></fileName>
<contentLength>190522</contentLength>
</item>
</channel>
</rss>`

func parseFixture(t *testing.T) []store.Item {
	t.Helper()
	items, err := ParseFeed([]byte(fixtureFeed), "rss", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestParseFeed(t *testing.T) {
	items := parseFixture(t)
	if len(items) != 5 {
		t.Fatalf("parsed %d items, want 5", len(items))
	}
	uhd := items[0]
	if uhd.GroupID != "1154515" || uhd.SizeBytes != 25422398398 {
		t.Errorf("uhd item = %+v", uhd)
	}
	if uhd.Filename != "scene-uhd.torrent" || uhd.Infohash != "AA11" {
		t.Errorf("nested torrent extension not parsed: %+v", uhd)
	}
	if len(uhd.Tags) == 0 || uhd.Tags[0] != "fuckpassvr.com" {
		t.Errorf("uhd tags = %v", uhd.Tags)
	}
	if !strings.Contains(uhd.EnclosureURL, "action=download") {
		t.Errorf("enclosure lost: %q", uhd.EnclosureURL)
	}
}

func TestDeriveCodecBadges(t *testing.T) {
	cases := []struct {
		tags []string
		want string
	}{
		{[]string{"h.265", "4096p"}, "h265"},
		{[]string{"x264", "1920p"}, "h264"},
		{[]string{"av1", "3840p"}, "av1"},
		{[]string{"4096p"}, ""},
	}
	for _, tc := range cases {
		if got := DeriveVariant(store.Item{Tags: tc.tags}).Codec; got != tc.want {
			t.Errorf("tags %v: codec = %q, want %q", tc.tags, got, tc.want)
		}
	}
}

func TestDerive1600pGearVR(t *testing.T) {
	v := DeriveVariant(store.Item{
		Title: "POVROriginals - Clowning Around - Bree Sky (2026.09.16) (GearVR)",
		Tags:  []string{"1600p", "samsung.gear.vr", "virtual.reality"},
	})
	if v.Height != 800 || v.Resolution != "1600p" {
		t.Errorf("gearvr variant = %+v", v)
	}
}

func TestNested2700pVariant(t *testing.T) {
	groups := Group(parseFixture(t))
	vs, ok := groups[GroupKey("CzechVR 894 - Always Have Time for You - Sofia Vega (2026.08.19) (Oculus 6K)")]
	if !ok || len(vs) != 1 {
		t.Fatalf("czech group = %+v", groups)
	}
	if vs[0].Height != 1350 || vs[0].Resolution != "2700p" {
		t.Errorf("2700p variant = %+v", vs[0])
	}
	if vs[0].Item.SizeBytes != 10881611442 {
		t.Errorf("2700p size = %d", vs[0].Item.SizeBytes)
	}
}

func TestGroupClustersVariants(t *testing.T) {
	groups := Group(parseFixture(t))
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2 (funscript must be excluded)", len(groups))
	}
	vs := groups[GroupKey("FuckPassVR - Rainy City Rendezvous - Mia James (2026.08.28) (Oculus 8K, UHD)")]
	if len(vs) != 3 {
		t.Fatalf("variants = %d, want 3", len(vs))
	}
	// Best-first: 8K UHD, 8K, 4K.
	if vs[0].Height != 2048 || !vs[0].HBR {
		t.Errorf("vs[0] = %+v, want 8K HBR first", vs[0])
	}
	if vs[1].Height != 1920 || vs[1].HBR {
		t.Errorf("vs[1] = %+v, want non-HBR 8K second", vs[1])
	}
	if vs[2].Height != 1024 {
		t.Errorf("vs[2] = %+v, want 4K last", vs[2])
	}
}

func TestProfilePick(t *testing.T) {
	groups := Group(parseFixture(t))
	vs := groups[GroupKey("FuckPassVR - Rainy City Rendezvous - Mia James (2026.08.28) (Oculus 8K, UHD)")]
	p := Profile{}
	if got := p.Pick(vs); got != 0 {
		t.Errorf("default pick = %d, want 0 (highest resolution wins)", got)
	}
	// At equal height, non-HBR wins by default and HBR wins when preferred.
	tied := []Variant{
		{Item: vs[0].Item, Height: 1920, HBR: true, Resolution: "3840p"},
		{Item: vs[1].Item, Height: 1920, HBR: false, Resolution: "3840p"},
	}
	if got := (Profile{}).Pick(tied); got != 1 {
		t.Errorf("default tied pick = %d, want 1 (non-HBR)", got)
	}
	if got := (Profile{PreferHBR: true}).Pick(tied); got != 0 {
		t.Errorf("hbr tied pick = %d, want 0", got)
	}
	capped := Profile{MaxHeight: 1024}
	if got := capped.Pick(vs); got != 2 {
		t.Errorf("capped pick = %d, want 2 (4K)", got)
	}
}

func TestGroupMergesReorderedPerformerTitles(t *testing.T) {
	items := []store.Item{
		{
			GroupID: "a", Title: "CzechVR 899 - Nerdy Girl Next-Door - Emejota (2026.09.07) (GearVR)",
			Tags: []string{"1440p", "virtual.reality"}, SizeBytes: 4 << 30,
			PubDate: time.Now().UTC(), FetchedAt: time.Now().UTC(),
		},
		{
			GroupID: "b", Title: "CzechVR 899 - Emejota - Nerdy Girl Next-Door (8K)",
			Tags: []string{"4096p", "virtual.reality"}, SizeBytes: 18 << 30,
			PubDate: time.Now().UTC(), FetchedAt: time.Now().UTC(),
		},
	}
	groups := Group(items)
	if len(groups) != 1 {
		keys := []string{}
		for k := range groups {
			keys = append(keys, k)
		}
		t.Fatalf("groups = %d %v, want 1 merged group", len(groups), keys)
	}
	for _, vs := range groups {
		if len(vs) != 2 {
			t.Fatalf("variants = %d, want 2", len(vs))
		}
		if vs[0].Height != 2048 {
			t.Errorf("first variant height = %d, want 8K first", vs[0].Height)
		}
	}
}

func TestGroupKeepsDifferentScenesApart(t *testing.T) {
	mk := func(id, title string) store.Item {
		return store.Item{GroupID: id, Title: title,
			Tags: []string{"4096p", "virtual.reality"}, SizeBytes: 18 << 30,
			PubDate: time.Now().UTC(), FetchedAt: time.Now().UTC()}
	}
	groups := Group([]store.Item{
		mk("a", "CzechVR 899 - Nerdy Girl Next-Door - Emejota (8K)"),
		mk("b", "CzechVR 900 - Different Scene Entirely - Other Girl (8K)"),
	})
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2 (899 vs 900 stay apart)", len(groups))
	}
}

func TestGroupKeyStripsVariantParenthetical(t *testing.T) {
	a := GroupKey("FuckPassVR - Rainy City Rendezvous - Mia James (2026.08.28) (Oculus 8K, UHD)")
	b := GroupKey("FuckPassVR - Rainy City Rendezvous - Mia James (2026.08.28) (Oculus, Go 4K)")
	if a != b {
		t.Errorf("keys differ:\n%q\n%q", a, b)
	}
}

// Reported bug: a bare "2K" outside any parenthetical survived in the
// group key and leaked into the Find-versions Jackett query, hiding the
// 4K/8K uploads of the same scene.
func TestGroupKeyStripsBareResolution(t *testing.T) {
	cases := []struct{ title, want string }{
		{"[Virtual Papi] SfizyDyd (Next Door Peep) 2K", "[virtual papi] sfizydyd (next door peep)"},
		{"Studio - Scene Name 4096p", "studio - scene name"},
		{"Studio - Scene Name UHD", "studio - scene name"},
		{"Studio - Scene Name 2K,", "studio - scene name"},
	}
	for _, tc := range cases {
		if got := GroupKey(tc.title); got != tc.want {
			t.Errorf("GroupKey(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
	// A title that is only a resolution token keeps it (never empty).
	if got := GroupKey("8K"); got != "8k" {
		t.Errorf("GroupKey(%q) = %q, want %q", "8K", got, "8k")
	}
}

// Trailing square-bracket variant blocks strip like parentheticals,
// but a parenthetical they uncover is identity and stays.
func TestGroupKeyStripsBracketVariants(t *testing.T) {
	cases := []struct{ title, want string }{
		{
			"[Virtual Papi] SfizyDyd (Next Door Peep) [VR, 60 FPS, 180°, 6K, 3072p] [Oculus Rift / Vive]",
			"[virtual papi] sfizydyd (next door peep)",
		},
		{"Studio - Scene Name [4K]", "studio - scene name"},
		{"Studio - Scene Name [Oculus] (8K)", "studio - scene name"},
	}
	for _, tc := range cases {
		if got := GroupKey(tc.title); got != tc.want {
			t.Errorf("GroupKey(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}

// The reported pair: a bare-2K upload and a bracketed-6K upload of the
// same scene cluster into one group under one resolution-free key.
func TestGroupClustersBracketAndBareVariants(t *testing.T) {
	mk := func(id, title string) store.Item {
		return store.Item{GroupID: id, Title: title,
			Tags: []string{"virtual.reality"}, SizeBytes: 4 << 30,
			PubDate: time.Now().UTC(), FetchedAt: time.Now().UTC()}
	}
	groups := Group([]store.Item{
		mk("a", "[Virtual Papi] SfizyDyd (Next Door Peep) 2K"),
		mk("b", "[Virtual Papi] SfizyDyd (Next Door Peep) [VR, 60 FPS, 180°, 6K, 3072p] [Oculus Rift / Vive]"),
	})
	if len(groups) != 1 {
		keys := []string{}
		for k := range groups {
			keys = append(keys, k)
		}
		t.Fatalf("groups = %d %v, want 1", len(groups), keys)
	}
	for k, vs := range groups {
		if k != "[virtual papi] sfizydyd (next door peep)" {
			t.Errorf("key = %q, want the shared identity key", k)
		}
		if len(vs) != 2 {
			t.Errorf("variants = %d, want 2", len(vs))
		}
	}
}

// Same scene uploaded at two bare resolutions clusters into one group.
func TestGroupClustersBareResolutionVariants(t *testing.T) {
	mk := func(id, title string) store.Item {
		return store.Item{GroupID: id, Title: title,
			Tags: []string{"virtual.reality"}, SizeBytes: 4 << 30,
			PubDate: time.Now().UTC(), FetchedAt: time.Now().UTC()}
	}
	groups := Group([]store.Item{
		mk("a", "[Virtual Papi] SfizyDyd (Next Door Peep) 2K"),
		mk("b", "[Virtual Papi] SfizyDyd (Next Door Peep) 4K"),
	})
	if len(groups) != 1 {
		keys := []string{}
		for k := range groups {
			keys = append(keys, k)
		}
		t.Fatalf("groups = %d %v, want 1 (resolutions cluster)", len(groups), keys)
	}
	for k, vs := range groups {
		if k != "[virtual papi] sfizydyd (next door peep)" {
			t.Errorf("key = %q, want resolution-free key", k)
		}
		if len(vs) != 2 {
			t.Errorf("variants = %d, want 2", len(vs))
		}
	}
}

// Reported bug: a Jackett-sourced "(Oculus 8K, HQ)" upload arrived with
// no usable tags (only the Torznab category) and was shown without the
// HBR badge. Title tokens must backstop the tag check.
func TestDeriveHBRFromTitle(t *testing.T) {
	cases := []struct {
		title string
		tags  []string
		want  bool
	}{
		{"PornCornVR - Anal Enjoyment with Lauren Phillips (2026.01.17) (Oculus 8K, HQ)", []string{"cat:100002"}, true},
		{"Studio - Scene (2026.01.01) (Oculus 8K, HBR)", nil, true},
		{"Studio - Scene (2026.01.01) (Oculus 8K, High Bitrate)", nil, true},
		{"Studio - Scene (2026.01.01) (Oculus 8K)", nil, false},
		{"Studio - Scene (2026.01.01) (Oculus 8K, UHD)", nil, false},
	}
	for _, tc := range cases {
		if got := DeriveVariant(store.Item{Title: tc.title, Tags: tc.tags}).HBR; got != tc.want {
			t.Errorf("%q: HBR = %v, want %v", tc.title, got, tc.want)
		}
	}
	// Tag-only signals still work with a bare title.
	if got := DeriveVariant(store.Item{Title: "Studio - Scene", Tags: []string{"high.bitrate"}}).HBR; !got {
		t.Error("high.bitrate tag: HBR = false, want true")
	}
}
