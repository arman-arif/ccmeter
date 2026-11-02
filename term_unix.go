//go:build !windows

package main

// enableTerminalColor is a no-op: Unix terminals understand ANSI as they are.
func enableTerminalColor() bool { return true }
