package exec

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/argoproj/pkg/rand"
)

var ErrWaitPIDTimeout = fmt.Errorf("Timed out waiting for PID to complete")

type CmdOpts struct {
	timeout time.Duration
}

var DefaultCmdOpts = CmdOpts{
	timeout: time.Duration(0),
}

// RunCommandExt is a convenience function to run/log a command and return/log stderr in an error upon
// failure.
func RunCommandExt(cmd *exec.Cmd, opts CmdOpts) (string, error) {

	logCtx := log.WithFields(log.Fields{"execID": rand.RandString(5)})
	// log in a way we can copy-and-paste into a terminal
	args := strings.Join(cmd.Args, " ")
	logCtx.WithFields(log.Fields{"dir": cmd.Dir}).Info(args)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Start()
	if err != nil {
		return "", err
	}

	done := make(chan error)
	go func() { done <- cmd.Wait() }()

	// Start a timer
	timeout := DefaultCmdOpts.timeout

	if opts.timeout != time.Duration(0) {
		timeout = opts.timeout
	}

	var timoutCh <-chan time.Time
	if timeout != 0 {
		timoutCh = time.NewTimer(timeout).C
	}

	select {
	//noinspection ALL
	case <- timoutCh:
		_ = cmd.Process.Kill()
		output := stdout.String()
		logCtx.WithFields(log.Fields{"duration": time.Since(start)}).Debug(output)
		err = fmt.Errorf("`%v` timeout after %v", args, timeout)
		logCtx.Error(err)
		return strings.TrimSpace(output), err
	case err := <-done:
		if err != nil {
			output := stdout.String()
			logCtx.WithFields(log.Fields{"duration": time.Since(start)}).Debug(output)
			err := fmt.Errorf("`%v` failed: %v", args, strings.TrimSpace(stderr.String()))
			logCtx.Error(err)
			return strings.TrimSpace(output), err
		}
	}

	output := stdout.String()
	logCtx.WithFields(log.Fields{"duration": time.Since(start)}).Debug(output)

	return strings.TrimSpace(output), nil
}

func RunCommand(name string, opts CmdOpts, arg ...string) (string, error) {
	return RunCommandExt(exec.Command(name, arg...), opts)
}

// WaitPIDOpts are options to WaitPID
type WaitPIDOpts struct {
	PollInterval time.Duration
	Timeout      time.Duration
}

// WaitPID waits for a non-child process id to exit
func WaitPID(pid int, opts ...WaitPIDOpts) error {
	if runtime.GOOS != "linux" {
		return errors.Errorf("Platform '%s' unsupported", runtime.GOOS)
	}
	var timeout time.Duration
	var pollInterval = time.Second
	if len(opts) > 0 {
		if opts[0].PollInterval != 0 {
			pollInterval = opts[0].PollInterval
		}
		if opts[0].Timeout != 0 {
			timeout = opts[0].Timeout
		}
	}
	path := fmt.Sprintf("/proc/%d", pid)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var timoutCh <-chan time.Time
	if timeout != 0 {
		timoutCh = time.NewTimer(timeout).C
	}
	for {
		select {
		case <-ticker.C:
			_, err := os.Stat(path)
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return errors.WithStack(err)
			}
		case <-timoutCh:
			return ErrWaitPIDTimeout
		}
	}
}
