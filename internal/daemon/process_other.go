//go:build !unix

package daemon

import (
	"errors"
	"os"
	"os/exec"
)

var errUnsupported = errors.New("background execution requires a Unix platform")

func lockFile(*os.File) error {
	return errUnsupported
}

func detach(*exec.Cmd) error {
	return errUnsupported
}
