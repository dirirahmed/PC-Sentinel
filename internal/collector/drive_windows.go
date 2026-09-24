package collector

import (
	"strings"

	"golang.org/x/sys/windows"
)

// isLocalDrive excludes network shares and optical drives: querying them can
// block for seconds when the remote host is unreachable or no disc is present.
func isLocalDrive(mountpoint string) bool {
	p, err := windows.UTF16PtrFromString(strings.TrimRight(mountpoint, `\`) + `\`)
	if err != nil {
		return false
	}
	switch windows.GetDriveType(p) {
	case windows.DRIVE_FIXED, windows.DRIVE_REMOVABLE:
		return true
	}
	return false
}
