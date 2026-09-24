//go:build !windows

package collector

func isLocalDrive(string) bool { return true }
