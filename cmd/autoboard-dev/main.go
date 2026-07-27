package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

type childResult struct {
	name string
	err  error
}

func main() {
	os.Exit(run())
}

func run() int {
	daemon := exec.CommandContext(
		context.Background(),
		"go",
		"run",
		"./cmd/autoboard",
		"serve",
	)
	daemon.Env = append(os.Environ(), "AUTOBOARD_DEVELOPMENT=1")
	web := exec.CommandContext(
		context.Background(),
		"corepack",
		"pnpm",
		"--filter",
		"@autoboard/web",
		"dev",
	)
	children := map[string]*exec.Cmd{"daemon": daemon, "web": web}
	results := make(chan childResult, len(children))
	for name, command := range children {
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		command.Stdin = os.Stdin
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := command.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "start %s: %v\n", name, err)
			stopChildren(children, syscall.SIGTERM)
			return 1
		}
		go func(name string, command *exec.Cmd) {
			results <- childResult{name: name, err: command.Wait()}
		}(name, command)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	var exitCode int
	remaining := len(children)
	select {
	case received := <-signals:
		receivedSignal, ok := received.(syscall.Signal)
		if !ok {
			fmt.Fprintf(os.Stderr, "unsupported signal %T\n", received)
			exitCode = 1
		} else {
			exitCode = 128 + int(receivedSignal)
		}
	case result := <-results:
		remaining--
		if result.err == nil {
			fmt.Fprintf(
				os.Stderr,
				"%s exited unexpectedly; stopping its peer\n",
				result.name,
			)
			exitCode = 1
		} else if exitError := new(exec.ExitError); errors.As(
			result.err,
			&exitError,
		) {
			exitCode = exitError.ExitCode()
		} else {
			fmt.Fprintf(os.Stderr, "%s failed: %v\n", result.name, result.err)
			exitCode = 1
		}
	}
	stopChildren(children, syscall.SIGTERM)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for remaining > 0 {
		select {
		case <-results:
			remaining--
		case <-deadline.C:
			stopChildren(children, syscall.SIGKILL)
			return exitCode
		}
	}
	return exitCode
}

func stopChildren(children map[string]*exec.Cmd, signal syscall.Signal) {
	for _, command := range children {
		if command.Process == nil {
			continue
		}
		_ = syscall.Kill(-command.Process.Pid, signal)
	}
}
