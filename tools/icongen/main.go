// Command icongen renders the app icon as PNGs at the sizes an fnOS package
// needs. It uses only the standard library so it runs offline, and it renders
// at 4x then box-downsamples so the rounded corners stay smooth.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
)

const ss = 4 // supersampling factor

var (
	ink   = color.RGBA{0x23, 0x27, 0x2e, 0xff}
	white = color.RGBA{0xff, 0xff, 0xff, 0xff}
	line  = color.RGBA{0x23, 0x27, 0x2e, 0xff}
	fold  = color.RGBA{0xd7, 0xdb, 0xe0, 0xff}
)

type canvas struct {
	w, h int
	pix  []color.RGBA
}

func newCanvas(w, h int) *canvas {
	return &canvas{w: w, h: h, pix: make([]color.RGBA, w*h)}
}

func (c *canvas) set(x, y int, col color.RGBA) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}
	c.pix[y*c.w+x] = col
}

// fillRoundedRect paints a rounded rectangle, optionally overwriting only
// pixels where cur.a == 0 (used to carve shapes out of the background).
func (c *canvas) fillRoundedRect(x0, y0, x1, y1, r float64, col color.RGBA) {
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			if insideRounded(px, py, x0, y0, x1, y1, r) {
				c.set(x, y, col)
			}
		}
	}
}

func insideRounded(px, py, x0, y0, x1, y1, r float64) bool {
	if px < x0 || px > x1 || py < y0 || py > y1 {
		return false
	}
	dx := math.Max(math.Max(x0+r-px, px-(x1-r)), 0)
	dy := math.Max(math.Max(y0+r-py, py-(y1-r)), 0)
	return dx*dx+dy*dy <= r*r
}

type point struct{ x, y float64 }

// fillPolygon paints a simple polygon with the even-odd rule.
func (c *canvas) fillPolygon(pts []point, col color.RGBA) {
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, p := range pts {
		minY = math.Min(minY, p.y)
		maxY = math.Max(maxY, p.y)
	}
	y0 := int(math.Max(0, math.Floor(minY)))
	y1 := int(math.Min(float64(c.h-1), math.Ceil(maxY)))
	for y := y0; y <= y1; y++ {
		py := float64(y) + 0.5
		var xs []float64
		for i := 0; i < len(pts); i++ {
			a := pts[i]
			b := pts[(i+1)%len(pts)]
			if (a.y <= py) == (b.y <= py) {
				continue
			}
			t := (py - a.y) / (b.y - a.y)
			xs = append(xs, a.x+t*(b.x-a.x))
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			xa, xb := xs[i], xs[i+1]
			for x := int(math.Floor(xa)); x <= int(math.Ceil(xb)); x++ {
				px := float64(x) + 0.5
				if px >= xa && px <= xb {
					c.set(x, y, col)
				}
			}
		}
	}
}

func (c *canvas) downsample(factor int) *image.RGBA {
	w, h := c.w/factor, c.h/factor
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	n := float64(factor * factor)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var rs, gs, bs, as float64
			for dy := 0; dy < factor; dy++ {
				for dx := 0; dx < factor; dx++ {
					p := c.pix[(y*factor+dy)*c.w+(x*factor+dx)]
					a := float64(p.A)
					rs += float64(p.R) * a
					gs += float64(p.G) * a
					bs += float64(p.B) * a
					as += a
				}
			}
			if as == 0 {
				continue
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(rs/as + 0.5),
				G: uint8(gs/as + 0.5),
				B: uint8(bs/as + 0.5),
				A: uint8(as/n + 0.5),
			})
		}
	}
	return dst
}

// render draws the icon at size*ss and returns the downsampled image.
func render(size int) *image.RGBA {
	s := float64(size * ss)
	c := newCanvas(size*ss, size*ss)

	// Rounded-square background.
	c.fillRoundedRect(0, 0, s, s, s*0.22, ink)

	// Page with a cut corner.
	page := []point{
		{0.30 * s, 0.19 * s},
		{0.585 * s, 0.19 * s},
		{0.70 * s, 0.305 * s},
		{0.70 * s, 0.81 * s},
		{0.30 * s, 0.81 * s},
	}
	c.fillPolygon(page, white)

	// Folded corner triangle.
	foldTri := []point{
		{0.585 * s, 0.19 * s},
		{0.585 * s, 0.305 * s},
		{0.70 * s, 0.305 * s},
	}
	c.fillPolygon(foldTri, fold)

	// Text lines.
	lx0, lx1 := 0.382*s, 0.618*s
	lh := 0.045 * s
	for i, frac := range []float64{0.44, 0.545, 0.65} {
		y := frac * s
		x1 := lx1
		if i == 2 {
			x1 = 0.53 * s
		}
		c.fillRoundedRect(lx0, y, x1, y+lh, lh/2, line)
	}

	return c.downsample(ss)
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	return enc.Encode(f, img)
}

func main() {
	out := flag.String("out", "build/icons", "directory to write icons into")
	flag.Parse()

	for _, size := range []int{256, 128, 64} {
		img := render(size)
		path := filepath.Join(*out, fmt.Sprintf("icon_%d.png", size))
		if err := writePNG(path, img); err != nil {
			log.Fatalf("write %s: %v", path, err)
		}
		fmt.Println("wrote", path)
	}
}
