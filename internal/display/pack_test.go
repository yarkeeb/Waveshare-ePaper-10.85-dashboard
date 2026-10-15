package display

import (
	"bytes"
	"image"
	"image/color"
	"math/rand"
	"testing"
)

// Hide the concrete type to exercise the original image.Image packing path.
type genericImage struct{ image.Image }

func TestPackGrayMatchesGeneric(t *testing.T) {
	for _, bounds := range []image.Rectangle{
		image.Rect(0, 0, Width, Height),
		image.Rect(13, 7, 13+Width, 7+Height),
		image.Rect(-13, -7, Width-13, Height-7),
	} {
		for _, pattern := range []string{"black", "white", "all byte values", "random"} {
			t.Run(bounds.String()+"/"+pattern, func(t *testing.T) {
				// A subimage exercises both a nonzero pixel offset in the parent and
				// padding at the end of each row, including on the final scanline.
				parent := image.NewGray(bounds.Inset(-11))
				for i := range parent.Pix {
					parent.Pix[i] = 0x55
				}
				img := parent.SubImage(bounds).(*image.Gray)
				rng := rand.New(rand.NewSource(42))
				for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
					for x := bounds.Min.X; x < bounds.Max.X; x++ {
						var v byte
						switch pattern {
						case "white":
							v = 255
						case "all byte values":
							v = byte((y-bounds.Min.Y)*Width + x - bounds.Min.X)
						case "random":
							v = byte(rng.Intn(256))
						}
						img.SetGray(x, y, color.Gray{Y: v})
					}
				}
				before := append([]byte(nil), parent.Pix...)
				got, err := Pack(img)
				if err != nil {
					t.Fatal(err)
				}
				want, err := Pack(genericImage{img})
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, want) {
					t.Fatal("grayscale fast path differs from original packing")
				}
				if !bytes.Equal(parent.Pix, before) {
					t.Fatal("packing mutated source pixels")
				}
			})
		}
	}
}

func TestPackGrayAllBitPatterns(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, Width, Height))
	// Every output byte, with the two grayscale values adjacent to the cutoff.
	for i := 0; i < 256; i++ {
		for bit := 0; bit < 8; bit++ {
			value := byte(127)
			if i&(0x80>>bit) != 0 {
				value = 128
			}
			img.Pix[i*8+bit] = value
		}
	}
	got, err := Pack(img)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 256; i++ {
		if got[i] != byte(i) {
			t.Fatalf("pattern %02x packed as %02x", i, got[i])
		}
	}
}

func BenchmarkPackGray(b *testing.B) {
	img := white()
	b.ReportAllocs()
	b.SetBytes(int64(len(img.Pix)))
	for b.Loop() {
		if _, err := Pack(img); err != nil {
			b.Fatal(err)
		}
	}
}
