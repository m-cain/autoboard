package launchagent

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type CommandLauncher struct {
	Stdout io.Writer
	Stderr io.Writer
}

func (CommandLauncher) Bootstrap(
	ctx context.Context,
	arguments ...string,
) error {
	command := launchctlCommand(ctx, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"%w: %s",
			err,
			strings.TrimSpace(string(output)),
		)
	}
	return nil
}

func (l CommandLauncher) Run(
	ctx context.Context,
	arguments ...string,
) error {
	command := launchctlCommand(ctx, arguments...)
	command.Stdout = l.Stdout
	command.Stderr = l.Stderr
	return command.Run()
}

func (CommandLauncher) Loaded(
	ctx context.Context,
	arguments ...string,
) (bool, error) {
	command := launchctlCommand(ctx, arguments...)
	output, err := command.CombinedOutput()
	if err == nil {
		return true, nil
	}
	if strings.Contains(string(output), "Could not find service") {
		return false, nil
	}
	return false, fmt.Errorf(
		"launchctl %s: %w: %s",
		strings.Join(arguments, " "),
		err,
		strings.TrimSpace(string(output)),
	)
}

func launchctlCommand(ctx context.Context, arguments ...string) *exec.Cmd {
	//nolint:gosec // launchctl is fixed and arguments come from the lifecycle manager.
	return exec.CommandContext(ctx, "launchctl", arguments...)
}
