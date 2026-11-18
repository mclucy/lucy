package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/mclucy/lucy/cmd"
	"github.com/mclucy/lucy/internal/cli"
)

func main() {
	ctx, stop := signalContext()
	directory, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "determine invocation directory: %v\n", err)
		stop()
		os.Exit(1)
	}
	code := run(ctx, os.Args[1:], directory, os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// signalCancelCause marks a cancellation that came from the operating system.
// It implements Is(context.Canceled) so existing exit handling is unchanged,
// and SignalCancellation lets command telemetry distinguish an actual signal
// from generic internal cancellation without exposing its description.
type signalCancelCause struct{}

func (signalCancelCause) Error() string {
	return "signal received"
}

func (signalCancelCause) Is(target error) bool {
	return target == context.Canceled
}

func (signalCancelCause) SignalCancellation() {}

// signalContext owns signal handling at the process boundary. The returned
// context carries a typed cause so telemetry can classify interruption only
// when a signal actually arrived.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-signals:
			cancel(signalCancelCause{})
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		signal.Stop(signals)
		cancel(nil)
	}
}

func run(ctx context.Context, args []string, directory string, stdin io.Reader, stdout, stderr io.Writer) (exitCode int) {
	runner := cmd.NewRunner(args, cmd.RunnerOptions{
		Directory: directory,
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stderr,
	})
	executeErr := runner.Execute(ctx)
	closeErr := runner.Close()
	if executeErr == nil && closeErr == nil {
		executeErr = runner.FlushOutput()
	}
	var exitErr *cli.ExitError
	if errors.As(executeErr, &exitErr) {
		if closeErr != nil {
			if runner.JSONSelected() {
				runner.WriteError(errors.Join(exitErr.Err, closeErr))
			} else {
				runner.WriteError(closeErr)
			}
			return 1
		}
		if exitErr.Silent {
			if flushErr := runner.FlushOutput(); flushErr != nil {
				runner.WriteError(flushErr)
				return 2
			}
			return exitErr.Code
		}
		if runner.JSONSelected() {
			runner.WriteError(exitErr.Err)
		}
		return exitErr.Code
	}
	switch {
	case runner.JSONSelected():
		// One document on stderr. A close failure joins the action failure
		// instead of silently disappearing or producing a second document.
		if err := errors.Join(executeErr, closeErr); err != nil {
			runner.WriteError(err)
		}
	case closeErr != nil:
		runner.WriteError(closeErr)
	}
	switch {
	case executeErr == nil && closeErr == nil:
		return 0
	case errors.Is(executeErr, context.Canceled):
		return 130
	default:
		return 1
	}
}
