// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package osutil

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/strutil"
)

var (
	syscallKill    = syscall.Kill
	syscallGetpgid = syscall.Getpgid
)

var cmdWaitTimeout = 5 * time.Second

// KillProcessGroup kills the process group associated with the given command.
//
// If the command hasn't had Setpgid set in its SysProcAttr, you'll probably end
// up killing yourself.
func KillProcessGroup(cmd *exec.Cmd) error {
	pgid := mylog.Check2(syscallGetpgid(cmd.Process.Pid))

	if pgid == 1 {
		return fmt.Errorf("cannot kill pgid 1")
	}
	return syscallKill(-pgid, syscall.SIGKILL)
}

// RunAndWait runs a command for the given argv with the given environ added to
// os.Environ, killing it if it reaches timeout, or if the tomb is dying.
func RunAndWait(argv []string, env []string, timeout time.Duration, tomb *tomb.Tomb) ([]byte, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("internal error: osutil.RunAndWait needs non-empty argv")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("internal error: osutil.RunAndWait needs positive timeout")
	}
	if tomb == nil {
		return nil, fmt.Errorf("internal error: osutil.RunAndWait needs non-nil tomb")
	}

	command := exec.Command(argv[0], argv[1:]...)

	// setup a process group for the command so that we can kill parent
	// and children on e.g. timeout
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Env = append(os.Environ(), env...)

	// Make sure we can obtain stdout and stderror. Same buffer so they're
	// combined.
	buffer := strutil.NewLimitedBuffer(100, 10*1024)
	command.Stdout = buffer
	command.Stderr = buffer
	mylog.Check(

		// Actually run the command.
		command.Start())

	// add timeout handling
	killTimerCh := time.After(timeout)

	commandCompleted := make(chan struct{})
	var commandError error
	go func() {
		// Wait for hook to complete
		commandError = command.Wait()
		close(commandCompleted)
	}()

	var abortOrTimeoutError error
	select {
	case <-commandCompleted:
		// Command completed; it may or may not have been successful.
		return buffer.Bytes(), commandError
	case <-tomb.Dying():
		// Hook was aborted, process will get killed below
		abortOrTimeoutError = fmt.Errorf("aborted")
	case <-killTimerCh:
		// Max timeout reached, process will get killed below
		abortOrTimeoutError = fmt.Errorf("exceeded maximum runtime of %s", timeout)
	}
	mylog.Check(

		// select above exited which means that aborted or killTimeout
		// was reached. Kill the command and wait for command.Wait()
		// to clean it up (but limit the wait with the cmdWaitTimer)
		KillProcessGroup(command))

	select {
	case <-time.After(cmdWaitTimeout):
		// cmdWaitTimeout was reached, i.e. command.Wait() did not
		// finish in a reasonable amount of time, we can not use
		// buffer in this case so return without it.
		return nil, fmt.Errorf("%v, but did not stop", abortOrTimeoutError)
	case <-commandCompleted:
		// cmd.Wait came back from waiting the killed process
		break
	}
	fmt.Fprintf(buffer, "\n<%s>", abortOrTimeoutError)

	return buffer.Bytes(), abortOrTimeoutError
}

type waitingReader struct {
	reader io.Reader
	cmd    *exec.Cmd
}

func (r *waitingReader) Close() error {
	if r.cmd.Process != nil {
		r.cmd.Process.Kill()
	}
	return r.cmd.Wait()
}

func (r *waitingReader) Read(b []byte) (int, error) {
	n, err := r.reader.Read(b)
	if n == 0 && err == io.EOF {
		err = r.Close()
		if err == nil {
			return 0, io.EOF
		}
		return 0, err
	}
	return n, err
}

// StreamCommand runs a the named program with the given arguments,
// streaming its standard output over the returned io.ReadCloser.
//
// The program will run until EOF is reached (at which point the
// ReadCloser is closed), or until the ReadCloser is explicitly closed.
func StreamCommand(name string, args ...string) (io.ReadCloser, error) {
	cmd := exec.Command(name, args...)
	pipe := mylog.Check2(cmd.StdoutPipe())

	cmd.Stderr = os.Stderr
	mylog.Check(cmd.Start())

	return &waitingReader{reader: pipe, cmd: cmd}, nil
}

// RunCmd runs a command and returns separately stdout and stderr
// output, and an error.
func RunCmd(c *exec.Cmd) ([]byte, []byte, error) {
	if c.Stdout != nil {
		return nil, nil, errors.New("osutil.Run: Stdout already set")
	}
	if c.Stderr != nil {
		return nil, nil, errors.New("osutil.Run: Stderr already set")
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	mylog.Check(c.Run())
	return stdout.Bytes(), stderr.Bytes(), err
}

// RunSplitOutput runs name command with arg arguments and returns
// stdout, stderr, and an error.
func RunSplitOutput(name string, arg ...string) ([]byte, []byte, error) {
	cmd := exec.Command(name, arg...)
	return RunCmd(cmd)
}
