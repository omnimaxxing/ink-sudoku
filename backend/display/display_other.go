//go:build !linux

package display

import "errors"

// Watch needs the tablet's journal.
func Watch(func(awake bool)) error { return errors.New("display: only on the tablet") }
