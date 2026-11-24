//go:build !linux && !windows

package i2p

import "os/exec"

func setParentDeathSignal(*exec.Cmd) {}
