// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package osutil_test

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type ctxSuite struct{}

type dumbReader struct{}

func (dumbReader) Read([]byte) (int, error) {
	return 1, nil
}

var _ = check.Suite(&ctxSuite{})

func (ctxSuite) TestWriter(c *check.C) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second/100)
	defer cancel()
	n, err := io.Copy(osutil.ContextWriter(ctx), dumbReader{})
	c.Assert(err, check.Equals, context.DeadlineExceeded)
	// but we copied things until the deadline hit
	c.Check(n, check.Not(check.Equals), int64(0))
}

func (ctxSuite) TestWriterDone(c *check.C) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n, err := io.Copy(osutil.ContextWriter(ctx), dumbReader{})
	c.Assert(err, check.Equals, context.Canceled)
	// and nothing was copied
	c.Check(n, check.Equals, int64(0))
}

func (ctxSuite) TestWriterSuccess(c *check.C) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second/100)
	defer cancel()
	// check we can copy if we're quick
	n, err := io.Copy(osutil.ContextWriter(ctx), strings.NewReader("hello"))
	c.Check(err, check.IsNil)
	c.Check(n, check.Equals, int64(len("hello")))
}

func (ctxSuite) TestRunMany(c *check.C) {
	var cmds []*exec.Cmd
	var tasks []func() error
	var taskRun uint32

	buildExec := func(ctx context.Context) ([]*exec.Cmd, []func() error, error) {
		// Successful commands
		cmds = []*exec.Cmd{
			exec.CommandContext(ctx, "true"),
			exec.CommandContext(ctx, "echo", "hello"),
		}
		// Successful tasks
		tasks = []func() error{
			func() error { atomic.AddUint32(&taskRun, 1); return nil },
			func() error { atomic.AddUint32(&taskRun, 1); return nil },
		}
		return cmds, tasks, nil
	}

	err := osutil.RunManyWithContext(context.Background(), buildExec)
	c.Assert(err, check.IsNil)
	atomic.LoadUint32(&taskRun)
	c.Check(taskRun, check.Equals, uint32(2))
	// ProcessState exists only if the process finished
	c.Assert(cmds[0].ProcessState, check.NotNil)
	c.Assert(cmds[1].ProcessState, check.NotNil)
	c.Assert(cmds[0].ProcessState.Success(), check.Equals, true)
	c.Assert(cmds[1].ProcessState.Success(), check.Equals, true)
}

func (ctxSuite) TestRunManyCmdError(c *check.C) {
	var cmds []*exec.Cmd
	buildExec := func(ctx context.Context) ([]*exec.Cmd, []func() error, error) {
		// One command will fail
		cmds = []*exec.Cmd{
			exec.CommandContext(ctx, "false"), // Returns exit status 1
			exec.CommandContext(ctx, "sleep", "10"),
		}
		return cmds, nil, nil
	}

	err := osutil.RunManyWithContext(context.Background(), buildExec)
	c.Assert(err, check.NotNil)
	c.Assert(err.Error(), check.Matches, ".*exit status 1")
	// ProcessState exists only if the process finished
	c.Assert(cmds[0].ProcessState, check.NotNil)
	c.Assert(cmds[1].ProcessState, check.NotNil)
	c.Assert(cmds[0].ProcessState.Success(), check.Equals, false)
	c.Assert(cmds[1].ProcessState.Success(), check.Equals, false)
}

func (ctxSuite) TestRunManyCmdCannotStart(c *check.C) {
	var cmds []*exec.Cmd
	buildExec := func(ctx context.Context) ([]*exec.Cmd, []func() error, error) {
		// One command will fail
		cmds = []*exec.Cmd{
			exec.CommandContext(ctx, "/non/existing/command"),
			exec.CommandContext(ctx, "sleep", "10"),
		}
		return cmds, nil, nil
	}

	err := osutil.RunManyWithContext(context.Background(), buildExec)
	c.Assert(err, check.NotNil)
	c.Assert(err.Error(), check.Matches, "fork/exec /non/existing/command: no such file or directory")
	// First command never started
	c.Assert(cmds[0].ProcessState, check.IsNil)
	c.Assert(cmds[1].ProcessState, check.NotNil)
	c.Assert(cmds[1].ProcessState.Success(), check.Equals, false)
}

func (ctxSuite) TestRunManyTaskError(c *check.C) {
	var taskCancelled uint32
	buildExec := func(ctx context.Context) ([]*exec.Cmd, []func() error, error) {
		tasks := []func() error{
			func() error { return errors.New("boom") },
			func() error {
				select {
				case <-ctx.Done():
					atomic.AddUint32(&taskCancelled, 1)
				case <-time.After(10 * time.Second):
					c.Error("cancel not received")
				}
				return nil
			},
		}
		return nil, tasks, nil
	}

	err := osutil.RunManyWithContext(context.Background(), buildExec)
	c.Assert(err, check.ErrorMatches, "boom")
	// Second go routine was cancelled
	atomic.LoadUint32(&taskCancelled)
	c.Assert(taskCancelled, check.Equals, uint32(1))
}

func (ctxSuite) TestRunManyCanceled(c *check.C) {
	bgCtx, cancel := context.WithCancel(context.Background())

	var taskCancelled uint32
	buildExec := func(ctx context.Context) ([]*exec.Cmd, []func() error, error) {
		cmds := []*exec.Cmd{
			exec.CommandContext(ctx, "sleep", "10"),
		}
		tasks := []func() error{
			func() error {
				select {
				case <-ctx.Done():
					atomic.AddUint32(&taskCancelled, 1)
				case <-time.After(10 * time.Second):
					c.Error("cancel not received")
				}
				return nil
			},
		}
		return cmds, tasks, nil
	}

	// Cancel the context shortly after starting
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := osutil.RunManyWithContext(bgCtx, buildExec)
	// errgroup.WithContext can return the exit status of the process
	// (which was killed), or context.Canceled for the go routine
	c.Assert(err, check.NotNil)
	expErr := &exec.ExitError{}
	if err != context.Canceled && errors.As(err, &expErr) == false {
		c.Error("unexpected error type", err)
	}
	// The go routine was cancelled
	atomic.LoadUint32(&taskCancelled)
	c.Assert(taskCancelled, check.Equals, uint32(1))
}

func (ctxSuite) TestRunManyEmpty(c *check.C) {
	// Ensure it doesn't hang or crash with nil/empty inputs
	buildExec := func(ctx context.Context) ([]*exec.Cmd, []func() error, error) {
		return nil, nil, nil
	}
	err := osutil.RunManyWithContext(context.Background(), buildExec)
	c.Assert(err, check.IsNil)
}
