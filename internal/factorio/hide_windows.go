package factorio

import (
	"os/exec"
	"syscall"
)

// createNoWindow keeps a console window from flashing up while the game
// runs for a program that has none of its own.
const createNoWindow = 0x08000000

func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
