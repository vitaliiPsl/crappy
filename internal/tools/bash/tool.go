package bash

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/x/tool"
)

const (
	Name        = "bash"
	description = `Execute a shell command and return combined stdout and stderr output.

Use this tool to run shell commands, scripts, build tools, tests, and other executables.
Each command starts in the working directory. Directory changes apply only to that command.
Avoid commands that run indefinitely. Commands and any background processes they start are killed when execution is cancelled.`

	waitDelay = 2 * time.Second
)

type Input struct {
	Command     string `json:"command" jsonschema:"The shell command to execute"`
	Description string `json:"description" jsonschema:"A short description of what the command does"`
}

func New(workdir string) kit.Tool {
	return tool.MustNew(
		Name,
		description,
		func(run *kit.RunContext, input Input) (string, error) {
			return execute(run.Context, workdir, input.Command)
		},
	)
}

func execute(ctx context.Context, workdir, command string) (string, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
	}

	cmd := exec.CommandContext(ctx, shell, "-c", command)
	cmd.Dir = workdir
	cmd.WaitDelay = waitDelay
	isolateProcessGroup(cmd)

	var output bytes.Buffer

	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		return "", err
	}

	pid := cmd.Process.Pid
	err := cmd.Wait()
	_ = killProcessGroup(pid)

	result := strings.TrimRight(output.String(), "\n")
	if errors.Is(err, exec.ErrWaitDelay) {
		return result, nil
	}

	if err != nil {
		if result != "" {
			return "", fmt.Errorf("%s\n%w", result, err)
		}

		return "", err
	}

	return result, nil
}
