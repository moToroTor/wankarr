// Command icon generates the Wankarr package icons (72px + 256px PNG):
// dark rounded square with a "W" glyph. Stdlib only.
// Run: go run ./tools/icon <outdir>
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: icon <outdir>")
		os.Exit(1)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(dir string) error {
	if err := writeIcon(filepath.Join(dir, "PACKAGE_ICON.PNG"), 72); err != nil {
		return err
	}
	return writeIcon(filepath.Join(dir, "PACKAGE_ICON_256.PNG"), 256)
}

func writeIcon(path string, size int) error {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg := color.RGBA{0x1b, 0x1e, 0x23, 0xff}
	fg := color.RGBA{0x7f, 0xae, 0x7f, 0xff}
	r := float64(size) * 0.22
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if rounded(x, y, size, r) {
				img.Set(x, y, bg)
			}
		}
	}
	// "W" as four strokes in a unit box, scaled to the icon.
	ux := float64(size) / 100.0
	strokes := [][4]float64{
		{22, 28, 36, 72}, {36, 72, 50, 46}, {50, 46, 64, 72}, {64, 72, 78, 28},
	}
	w := int(4 * ux)
	if w < 1 {
		w = 1
	}
	for _, s := range strokes {
		line(img, int(s[0]*ux), int(s[1]*ux), int(s[2]*ux), int(s[3]*ux), w, fg)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func rounded(x, y, size int, r float64) bool {
	fx, fy, n := float64(x), float64(y), float64(size)
	if fx < 0 || fx >= n || fy < 0 || fy >= n {
		return false
	}
	for _, c := range [][2]float64{{r, r}, {n - r, r}, {r, n - r}, {n - r, n - r}} {
		dx, dy := fx-c[0], fy-c[1]
		edgeX := (fx < r && c[0] == r) || (fx >= n-r && c[0] == n-r)
		edgeY := (fy < r && c[1] == r) || (fy >= n-r && c[1] == n-r)
		if edgeX && edgeY && dx*dx+dy*dy > r*r {
			return false
		}
	}
	return true
}

func line(img *image.RGBA, x0, y0, x1, y1, w int, fg color.Color) {
	dx := x1 - x0
	dy := y1 - y0
	steps := abs(dx)
	if abs(dy) > steps {
		steps = abs(dy)
	}
	if steps == 0 {
		steps = 1
	}
	for i := 0; i <= steps; i++ {
		x := x0 + dx*i/steps
		y := y0 + dy*i/steps
		for oy := -w / 2; oy <= w/2; oy++ {
			for ox := -w / 2; ox <= w/2; ox++ {
				img.Set(x+ox, y+oy, fg)
			}
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
