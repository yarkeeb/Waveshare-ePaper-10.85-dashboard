//go:build linux

package display

import (
	"errors"
	"github.com/warthog618/go-gpiocdev"
	"github.com/yarkeeb/Waveshare-ePaper-10.85-dashboard/internal/config"
	"golang.org/x/sys/unix"
	"os"
	"unsafe"
)

type linuxBus struct {
	reset, dc, power, busy *gpiocdev.Line
	spi                    [2]*os.File
}

func Open(cfg config.Config) (Bus, error) {
	b := &linuxBus{}
	var err error
	defer func() {
		if err != nil {
			_ = b.Close()
		}
	}()
	pins := []struct {
		offset int
		dest   **gpiocdev.Line
	}{{cfg.Panel.Reset, &b.reset}, {cfg.Panel.DC, &b.dc}, {cfg.Panel.Power, &b.power}}
	for _, p := range pins {
		*p.dest, err = gpiocdev.RequestLine(cfg.Panel.GPIOChip, p.offset, gpiocdev.AsOutput(0), gpiocdev.WithConsumer("epaper-dashboard"))
		if err != nil {
			return nil, err
		}
	}
	b.busy, err = gpiocdev.RequestLine(cfg.Panel.GPIOChip, cfg.Panel.Busy, gpiocdev.AsInput, gpiocdev.WithConsumer("epaper-dashboard"))
	if err != nil {
		return nil, err
	}
	for i, path := range []string{cfg.Panel.SPIMaster, cfg.Panel.SPISlave} {
		b.spi[i], err = os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return nil, err
		}
		mode := uint8(0)
		bits := uint8(8)
		speed := uint32(4000000)
		for _, v := range []struct {
			req uintptr
			ptr unsafe.Pointer
		}{{0x40016b01, unsafe.Pointer(&mode)}, {0x40016b03, unsafe.Pointer(&bits)}, {0x40046b04, unsafe.Pointer(&speed)}} {
			_, _, errno := unix.Syscall(unix.SYS_IOCTL, b.spi[i].Fd(), v.req, uintptr(v.ptr))
			if errno != 0 {
				err = errno
				return nil, err
			}
		}
	}
	return b, nil
}
func bit(v bool) int {
	if v {
		return 1
	}
	return 0
}
func (b *linuxBus) Reset(v bool) error  { return b.reset.SetValue(bit(v)) }
func (b *linuxBus) DC(v bool) error     { return b.dc.SetValue(bit(v)) }
func (b *linuxBus) Power(v bool) error  { return b.power.SetValue(bit(v)) }
func (b *linuxBus) Busy() (bool, error) { v, e := b.busy.Value(); return v == 1, e }
func (b *linuxBus) Write(i int, data []byte) error {
	for len(data) > 0 {
		n := min(4096, len(data))
		written, e := b.spi[i].Write(data[:n])
		if e != nil {
			return e
		}
		if written != n {
			return errors.New("short SPI write")
		}
		data = data[n:]
	}
	return nil
}
func (b *linuxBus) Close() error {
	var errs []error
	for _, f := range b.spi {
		if f != nil {
			errs = append(errs, f.Close())
		}
	}
	for _, l := range []*gpiocdev.Line{b.reset, b.dc, b.power, b.busy} {
		if l != nil {
			errs = append(errs, l.Close())
		}
	}
	return errors.Join(errs...)
}
