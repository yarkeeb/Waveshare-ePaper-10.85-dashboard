// Driver sequence adapted from Waveshare's bundled epd10in85.py.
// Copyright/permission notice: see third_party/waveshare-LICENSE.
package display

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"time"
)

const Width = 1360
const Height = 480
const halfBytes = Width / 16 * Height

// Bus owns GPIO and SPI. Calls are serialized by Panel.
type Bus interface {
	Reset(bool) error
	DC(bool) error
	Power(bool) error
	Busy() (bool, error)
	Write(int, []byte) error
	Close() error
}
type Panel struct {
	bus      Bus
	previous [2][]byte
	partial  bool
	reinit   bool
	sleep    func(context.Context, time.Duration) error
}

func New(bus Bus, reinit bool) *Panel { return &Panel{bus: bus, reinit: reinit, sleep: pause} }
func pause(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (p *Panel) command(controller int, cmd byte, data ...byte) error {
	for i := 0; i < 2; i++ {
		if controller >= 0 && controller != i {
			continue
		}
		if e := p.bus.DC(false); e != nil {
			return e
		}
		if e := p.bus.Write(i, []byte{cmd}); e != nil {
			return e
		}
		if len(data) > 0 {
			if e := p.bus.DC(true); e != nil {
				return e
			}
			if e := p.bus.Write(i, data); e != nil {
				return e
			}
		}
	}
	return nil
}
func (p *Panel) wait(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		idle, e := p.bus.Busy()
		if e != nil {
			return e
		}
		if idle {
			return nil
		}
		if e = p.sleep(ctx, 100*time.Millisecond); e != nil {
			return fmt.Errorf("panel BUSY timeout: %w", e)
		}
	}
}
func (p *Panel) init(ctx context.Context, partial bool) error {
	if e := p.bus.Power(true); e != nil {
		return e
	}
	for _, s := range []struct {
		high  bool
		delay time.Duration
	}{{true, 200 * time.Millisecond}, {false, 2 * time.Millisecond}, {true, 200 * time.Millisecond}} {
		if e := p.bus.Reset(s.high); e != nil {
			return e
		}
		if e := p.sleep(ctx, s.delay); e != nil {
			return e
		}
	}
	if e := p.wait(ctx, 40*time.Second); e != nil {
		return e
	}
	seq := [][]byte{{0x4d, 0x55}, {0xa6, 0x38}, {0xb4, 0x5d}, {0xb6, 0x80}, {0xb7, 0}, {0xf7, 2}, {0xae, 0xa0}, {0xe0, 1}, {0, 0x9f, 0x0d}, {6, 0x57, 0x24, 0x28, 0x32, 8, 0x48}, {0x61, 2, 0xa8, 1, 0xe0}, {0x62, 0, 0, 0, 0}}
	if partial {
		seq = append(seq, []byte{0x82, 0x12})
	}
	seq = append(seq, []byte{0x60, 0x31}, []byte{0x50, 0x97}, []byte{0xe8, 1})
	if partial {
		seq = append(seq, []byte{0xe0, 3}, []byte{0xe5, 0x64})
	}
	seq = append(seq, []byte{4})
	for _, v := range seq {
		if e := p.command(-1, v[0], v[1:]...); e != nil {
			return e
		}
	}
	if e := p.sleep(ctx, 200*time.Millisecond); e != nil {
		return e
	}
	if e := p.wait(ctx, 40*time.Second); e != nil {
		return e
	}
	p.partial = partial
	return nil
}
func Pack(img image.Image) ([]byte, error) {
	if img.Bounds().Dx() != Width || img.Bounds().Dy() != Height {
		return nil, errors.New("panel image must be 1360x480")
	}
	b := make([]byte, Width*Height/8)
	if gray, ok := img.(*image.Gray); ok {
		// Gray.RGBA expands each byte by 257, so the generic luminance
		// threshold is exactly its high bit. Pack eight pixels at once,
		// avoiding interface calls and repeated writes to the same byte.
		for y := 0; y < Height; y++ {
			row := gray.Pix[y*gray.Stride : y*gray.Stride+Width]
			dst := b[y*(Width/8) : (y+1)*(Width/8)]
			for x := range dst {
				p := row[x*8 : x*8+8]
				dst[x] = p[0]&0x80 | (p[1]>>1)&0x40 | (p[2]>>2)&0x20 | (p[3]>>3)&0x10 |
					(p[4]>>4)&0x08 | (p[5]>>5)&0x04 | (p[6]>>6)&0x02 | p[7]>>7
			}
		}
		return b, nil
	}
	min := img.Bounds().Min
	for y := 0; y < Height; y++ {
		for x := 0; x < Width; x++ {
			r, g, bl, _ := img.At(x+min.X, y+min.Y).RGBA()
			if (299*r+587*g+114*bl)/1000 >= 32768 {
				b[y*Width/8+x/8] |= 0x80 >> uint(x%8)
			}
		}
	}
	return b, nil
}
func Split(buf []byte) ([2][]byte, error) {
	var parts [2][]byte
	if len(buf) != Width*Height/8 {
		return parts, errors.New("invalid framebuffer length")
	}
	for i := range parts {
		parts[i] = make([]byte, halfBytes)
	}
	for y := 0; y < Height; y++ {
		copy(parts[0][y*85:(y+1)*85], buf[y*170:y*170+85])
		copy(parts[1][y*85:(y+1)*85], buf[y*170+85:(y+1)*170])
	}
	return parts, nil
}
func (p *Panel) Draw(ctx context.Context, img image.Image, full bool) (err error) {
	defer func() {
		if err != nil {
			p.previous = [2][]byte{}
			p.partial = false
		}
	}()
	buf, e := Pack(img)
	if e != nil {
		return e
	}
	parts, e := Split(buf)
	if e != nil {
		return e
	}
	full = full || p.previous[0] == nil
	if full || !p.partial || p.reinit {
		if e = p.init(ctx, !full); e != nil {
			return e
		}
	}
	white := bytes.Repeat([]byte{255}, halfBytes)
	for i := 0; i < 2; i++ {
		old := p.previous[i]
		if full || old == nil {
			old = white
		}
		if e = p.command(i, 0x10, old...); e != nil {
			return e
		}
		if e = p.command(i, 0x13, parts[i]...); e != nil {
			return e
		}
	}
	if e = p.command(-1, 0x12); e != nil {
		return e
	}
	if e = p.sleep(ctx, 100*time.Millisecond); e != nil {
		return e
	}
	timeout := 12 * time.Second
	if full {
		timeout = 40 * time.Second
	}
	if e = p.wait(ctx, timeout); e != nil {
		p.previous = [2][]byte{}
		return e
	}
	// Copy new data into old RAM on both controllers after a successful refresh.
	for i := 0; i < 2; i++ {
		if e = p.command(i, 0x10, parts[i]...); e != nil {
			p.previous = [2][]byte{}
			return e
		}
	}
	p.previous = parts
	return nil
}
func (p *Panel) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := p.command(-1, 2)
	if err == nil {
		err = p.sleep(ctx, 200*time.Millisecond)
	}
	if err == nil {
		err = p.wait(ctx, 4*time.Second)
	}
	if err == nil {
		err = p.command(-1, 7, 0xa5)
	}
	return errors.Join(err, p.bus.Power(false), p.bus.Close())
}
