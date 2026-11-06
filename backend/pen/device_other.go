//go:build !linux

package pen

import "errors"

// Device exists so the backend builds on the Mac; the digitizer is Linux-only.
type Device struct {
	Name   string
	Path   string
	Ranges Ranges
}

func Open() (*Device, error) { return nil, errors.New("pen: evdev is Linux-only") }

func (d *Device) Run(emit func(Event)) error { return errors.New("pen: evdev is Linux-only") }

func (d *Device) Close() error { return nil }
