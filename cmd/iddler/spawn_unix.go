//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// spawnEngine starts the engine as a detached process (its own session, no terminal).
func spawnEngine(background bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"--engine"}
	if background {
		args = append(args, "--background")
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
