//go:build !windows

package geoip

import "os"

func replaceFileAtomically(source, target string) error {
	return os.Rename(source, target)
}
