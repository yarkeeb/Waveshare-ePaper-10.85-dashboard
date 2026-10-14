package display

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"testing"
	"time"
)

type record struct {
	controller int
	data       bool
	bytes      []byte
}
type fakeBus struct {
	data    bool
	records []record
	idle    bool
	fail    bool
}

func (b *fakeBus) Reset(bool) error    { return nil }
func (b *fakeBus) DC(v bool) error     { b.data = v; return nil }
func (b *fakeBus) Power(bool) error    { return nil }
func (b *fakeBus) Busy() (bool, error) { return b.idle, nil }
func (b *fakeBus) Close() error        { return nil }
func (b *fakeBus) Write(i int, v []byte) error {
	if b.fail {
		return errors.New("SPI failed")
	}
	b.records = append(b.records, record{i, b.data, append([]byte(nil), v...)})
	return nil
}
func white() *image.Gray {
	im := image.NewGray(image.Rect(0, 0, Width, Height))
	draw.Draw(im, im.Bounds(), image.NewUniform(color.Gray{255}), image.Point{}, draw.Src)
	return im
}
func TestPackAndSplitAtControllerBoundary(t *testing.T) {
	im := white()
	im.SetGray(0, 0, color.Gray{0})
	im.SetGray(679, 0, color.Gray{0})
	im.SetGray(680, 0, color.Gray{0})
	im.SetGray(1359, 479, color.Gray{0})
	b, e := Pack(im)
	if e != nil {
		t.Fatal(e)
	}
	parts, e := Split(b)
	if e != nil {
		t.Fatal(e)
	}
	if len(parts[0]) != 40800 || parts[0][0] != 0x7f || parts[0][84] != 0xfe || parts[1][0] != 0x7f || parts[1][40799] != 0xfe {
		t.Fatal("incorrect MSB packing/controller split")
	}
	if _, e = Pack(image.NewGray(image.Rect(0, 0, 1, 1))); e == nil {
		t.Fatal("accepted wrong dimensions")
	}
}
func TestRefreshWritesBothOldAndNewMemories(t *testing.T) {
	b := &fakeBus{idle: true}
	p := New(b, false)
	p.sleep = func(context.Context, time.Duration) error { return nil }
	first := white()
	first.SetGray(1, 0, color.Gray{0})
	if e := p.Draw(context.Background(), first, false); e != nil {
		t.Fatal(e)
	}
	prev := append([]byte(nil), p.previous[0]...)
	b.records = nil
	next := white()
	next.SetGray(2, 0, color.Gray{0})
	if e := p.Draw(context.Background(), next, false); e != nil {
		t.Fatal(e)
	}
	var old, newData [][]byte
	for i, r := range b.records {
		if !r.data && r.controller == 0 && len(r.bytes) == 1 && i+1 < len(b.records) {
			switch r.bytes[0] {
			case 0x10:
				old = append(old, b.records[i+1].bytes)
			case 0x13:
				newData = append(newData, b.records[i+1].bytes)
			}
		}
	}
	if len(old) != 2 || len(newData) != 1 || !bytes.Equal(old[0], prev) || !bytes.Equal(old[1], newData[0]) {
		t.Fatal("partial refresh must send previous image then copy refreshed image to old RAM")
	}
}
func TestBusyTimeoutInvalidatesFrame(t *testing.T) {
	b := &fakeBus{idle: true}
	p := New(b, false)
	p.sleep = func(context.Context, time.Duration) error { return nil }
	if e := p.Draw(context.Background(), white(), true); e != nil {
		t.Fatal(e)
	}
	p.partial = true
	b.idle = false
	p.sleep = func(ctx context.Context, d time.Duration) error { return context.DeadlineExceeded }
	if e := p.Draw(context.Background(), white(), false); e == nil {
		t.Fatal("BUSY timeout ignored")
	}
	if p.previous[0] != nil || p.partial {
		t.Fatal("failed frame must force full reinitialization")
	}
}
