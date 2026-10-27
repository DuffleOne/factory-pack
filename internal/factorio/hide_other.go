//go:build !windows

package factorio

import "os/exec"

func hide(*exec.Cmd) {}
