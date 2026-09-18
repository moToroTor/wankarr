package emp

import (
	"testing"

	"wankarr/internal/store"
)

func TestIsVR(t *testing.T) {
	cases := []struct {
		name  string
		title string
		tags  []string
		want  bool
	}{
		{
			"vr headset tokens in title",
			"[ARPorn] Lauren Phillips (Anatomy with Stepmom [Passthrough]) [VR, 60 FPS, 180°, 6K, 3072, Oculus Rift / Vive]",
			nil, true,
		},
		{
			"flat onlyfans title",
			"[Onlyfans] Molly Little",
			[]string{"onlyfans.com", "solo"}, false,
		},
		{
			"flat 2160p title",
			"[PornWorld] - Lauren Phillips Balls Deep DP Threesome (2026-06-14) [2160p]",
			[]string{"hardcore", "threesome"}, false,
		},
		{
			"vr tag only, plain title",
			"Some Scene Name (2026.01.01)",
			[]string{"virtual.reality", "180.degrees", "mia.james"}, true,
		},
		{
			"gearvr 1600p variant resolves",
			"POVROriginals - Clowning Around - Bree Sky (2026.09.16) (GearVR)",
			[]string{"1600p", "samsung.gear.vr", "virtual.reality"}, true,
		},
		{
			"cover must not match vr substring",
			"Cover Girl Special Edition",
			[]string{"magazine", "pictures"}, false,
		},
		{
			"vr to normal conversion is flat",
			"VR to Normal Pornstar Mix Minipack 18: Lauren Phillips, Tru Kait",
			[]string{"cat:100029"}, false,
		},
		{
			"vr2normal tag is flat",
			"Some Scene (2026.01.01) [VR, 180°]",
			[]string{"vr2normal"}, false,
		},
		{
			"real vr with normal-adjacent words still passes",
			"Normal Morning Passion - Jane Doe [VR, 180°, Oculus 8K]",
			nil, true,
		},
	}
	for _, tc := range cases {
		it := store.Item{Title: tc.title, Tags: tc.tags}
		if got := IsVR(it); got != tc.want {
			t.Errorf("%s: IsVR = %v, want %v", tc.name, got, tc.want)
		}
	}
}
