//go:build windows

package qrterm

import "golang.org/x/sys/windows"

// EnableANSI turns on escape-sequence processing for a Windows console handle.
// Windows Terminal has it on already; the classic console host needs it set,
// and a console that refuses it would print the escapes as literal garbage —
// callers must then skip the drawing and show the link alone.
func EnableANSI(fd uintptr) bool {
	handle := windows.Handle(fd)
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
