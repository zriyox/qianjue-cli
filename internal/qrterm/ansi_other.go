//go:build !windows

package qrterm

// EnableANSI reports whether escape sequences can be used. Unix terminals
// interpret them natively; whether fd is a terminal at all is the caller's check.
func EnableANSI(fd uintptr) bool { return true }
