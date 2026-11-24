//go:build !windows

package config

import "os"

func atomicConfigReplace(source, target string) error {
	return os.Rename(source, target)
}
