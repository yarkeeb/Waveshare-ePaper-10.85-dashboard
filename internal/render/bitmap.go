package render

import (
	"bytes"
	"encoding/binary"
	"errors"
	"golang.org/x/image/bmp"
	"image"
	"image/color"
)

// Some bundled icons use uncompressed RGB555, which x/image/bmp does not decode.
func decodeIcon(b []byte) (image.Image, error) {
	im, e := bmp.Decode(bytes.NewReader(b))
	if e == nil {
		return im, nil
	}
	if len(b) < 54 || string(b[:2]) != "BM" || binary.LittleEndian.Uint32(b[14:18]) != 40 || binary.LittleEndian.Uint16(b[28:30]) != 16 || binary.LittleEndian.Uint32(b[30:34]) != 0 {
		return nil, e
	}
	w, h := int(int32(binary.LittleEndian.Uint32(b[18:22]))), int(int32(binary.LittleEndian.Uint32(b[22:26])))
	topDown := h < 0
	if topDown {
		h = -h
	}
	offset := int(binary.LittleEndian.Uint32(b[10:14]))
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 {
		return nil, errors.New("invalid icon dimensions")
	}
	stride := (w*2 + 3) &^ 3
	if offset < 54 || offset > len(b) || h*stride > len(b)-offset {
		return nil, errors.New("truncated icon")
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sourceY := h - 1 - y
		if topDown {
			sourceY = y
		}
		for x := 0; x < w; x++ {
			i := offset + sourceY*stride + x*2
			v := binary.LittleEndian.Uint16(b[i : i+2])
			out.SetRGBA(x, y, color.RGBA{R: uint8((v >> 10 & 31) * 255 / 31), G: uint8((v >> 5 & 31) * 255 / 31), B: uint8((v & 31) * 255 / 31), A: 255})
		}
	}
	return out, nil
}
