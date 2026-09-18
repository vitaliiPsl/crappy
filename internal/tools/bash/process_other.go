//go:build !unix

package bash

import "os/exec"

func isolateProcessGroup(*exec.Cmd) {}

func killProcessGroup(int) error { return nil }
