package pen

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

type Device struct {
	f      *os.File
	Name   string
	Path   string
	Ranges Ranges
}

// Open finds the stylus digitizer ("Elan marker input" on Paper Pro/Pure,
// a Wacom part on rM2) by name.
func Open() (*Device, error) {
	names, _ := filepath.Glob("/sys/class/input/event*/device/name")
	for _, n := range names {
		b, err := os.ReadFile(n)
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(b))
		lower := strings.ToLower(name)
		if !strings.Contains(lower, "marker") && !strings.Contains(lower, "wacom") {
			continue
		}
		node := filepath.Base(filepath.Dir(filepath.Dir(n)))
		return OpenPath("/dev/input/"+node, name)
	}
	return nil, errors.New("pen: no stylus digitizer found")
}

func OpenPath(path, name string) (*Device, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	d := &Device{f: f, Name: name, Path: path}
	for _, a := range []struct {
		code int
		dst  *AbsInfo
	}{{absX, &d.Ranges.X}, {absY, &d.Ranges.Y}, {absPressure, &d.Ranges.Pressure}} {
		info, err := absInfo(f, a.code)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("pen: EVIOCGABS(%#x): %w", a.code, err)
		}
		*a.dst = AbsInfo{Min: info[1], Max: info[2]}
	}
	return d, nil
}

// absInfo returns struct input_absinfo: value, minimum, maximum, fuzz, flat, resolution.
func absInfo(f *os.File, code int) ([6]int32, error) {
	var info [6]int32
	// EVIOCGABS(code) = _IOR('E', 0x40+code, struct input_absinfo)
	req := uintptr(2<<30) | unsafe.Sizeof(info)<<16 | 'E'<<8 | uintptr(0x40+code)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(unsafe.Pointer(&info)))
	if errno != 0 {
		return info, errno
	}
	return info, nil
}

// Run reads the device until it fails or is closed, calling emit for every
// Event. On 64-bit Linux, struct input_event is {timeval (2×int64), u16 type,
// u16 code, s32 value} = 24 bytes.
func (d *Device) Run(emit func(Event)) error {
	const size = 24
	buf := make([]byte, size*64)
	var dec decoder
	for {
		n, err := d.f.Read(buf)
		if err != nil {
			return err
		}
		for off := 0; off+size <= n; off += size {
			e := buf[off : off+size]
			dec.feed(
				int64(binary.LittleEndian.Uint64(e[0:])),
				int64(binary.LittleEndian.Uint64(e[8:])),
				binary.LittleEndian.Uint16(e[16:]),
				binary.LittleEndian.Uint16(e[18:]),
				int32(binary.LittleEndian.Uint32(e[20:])),
				emit,
			)
		}
	}
}

func (d *Device) Close() error { return d.f.Close() }
