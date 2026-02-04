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
	"testing"
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

func (ctxSuite) TestRun(c *check.C) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second/100)
	defer cancel()
	cmd := exec.Command("/bin/sleep", "1")
	err := osutil.RunWithContext(ctx, cmd)
	c.Check(err, check.Equals, context.DeadlineExceeded)
}

func (ctxSuite) TestRunRace(c *check.C) {
	if testing.Short() {
		c.Skip("skippinng non-short test")
	}

	// first, time how long /bin/false takes
	t0 := time.Now()
	cmderr := exec.Command("/bin/false").Run()
	dt := time.Since(t0)

	// note in particular the error is not "killed"
	c.Assert(cmderr, check.ErrorMatches, "exit status 1")
	failedstr := cmderr.Error()
	killedstr := context.DeadlineExceeded.Error()

	// now run it in a loop with a deadline of exactly that
	nkilled := 0
	nfailed := 0
	for nfailed == 0 || nkilled == 0 {
		cmd := exec.Command("/bin/false")
		ctx, cancel := context.WithTimeout(context.Background(), dt)
		err := osutil.RunWithContext(ctx, cmd)
		cancel()
		switch err.Error() {
		case killedstr:
			nkilled++
		case failedstr:
			nfailed++
		default:
			// if the error is anything other than due to the context
			// being done, or the command failing, there's a bug.
			c.Fatalf("expected %q or %q, got %q", failedstr, killedstr, err)
		}
	}
}

func (ctxSuite) TestRunDone(c *check.C) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := exec.Command("/bin/sleep", "1")
	err := osutil.RunWithContext(ctx, cmd)
	c.Check(err, check.Equals, context.Canceled)
}

func (ctxSuite) TestRunSuccess(c *check.C) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.Command("/bin/sleep", "0.01")
	err := osutil.RunWithContext(ctx, cmd)
	c.Check(err, check.IsNil)
}

func (ctxSuite) TestRunSuccessfulFailure(c *check.C) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.Command("not/something/you/can/run")
	err := osutil.RunWithContext(ctx, cmd)
	c.Check(err, check.ErrorMatches, `fork/exec \S+: no such file or directory`)
}

func (ctxSuite) TestRunMany(c *check.C) {
	ctx := context.Background()

	// Successful commands
	cmds := []*exec.Cmd{
		exec.Command("true"),
		exec.Command("echo", "hello"),
	}

	// Successful tasks
	var taskRun uint32
	tasks := []func(context.Context) error{
		func(context.Context) error { atomic.AddUint32(&taskRun, 1); return nil },
		func(context.Context) error { atomic.AddUint32(&taskRun, 1); return nil },
	}

	err := osutil.RunManyWithContext(ctx, cmds, tasks)
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
	ctx := context.Background()

	// One command will fail
	cmds := []*exec.Cmd{
		exec.Command("false"), // Returns exit status 1
		exec.Command("sleep", "10"),
	}

	err := osutil.RunManyWithContext(ctx, cmds, nil)
	c.Assert(err, check.NotNil)
	c.Assert(err.Error(), check.Matches, ".*exit status 1")
	// ProcessState exists only if the process finished
	c.Assert(cmds[0].ProcessState, check.NotNil)
	c.Assert(cmds[1].ProcessState, check.NotNil)
	c.Assert(cmds[0].ProcessState.Success(), check.Equals, false)
	c.Assert(cmds[1].ProcessState.Success(), check.Equals, false)
}

func (ctxSuite) TestRunManyCmdCannotStart(c *check.C) {
	ctx := context.Background()

	// One command will fail
	cmds := []*exec.Cmd{
		exec.Command("/non/existing/command"),
		exec.Command("sleep", "10"),
	}

	err := osutil.RunManyWithContext(ctx, cmds, nil)
	c.Assert(err, check.NotNil)
	c.Assert(err.Error(), check.Matches, "fork/exec /non/existing/command: no such file or directory")
	// First command never started
	c.Assert(cmds[0].ProcessState, check.IsNil)
	c.Assert(cmds[1].ProcessState, check.NotNil)
	c.Assert(cmds[1].ProcessState.Success(), check.Equals, false)
}

func (ctxSuite) TestRunManyTaskError(c *check.C) {
	ctx := context.Background()

	var taskCancelled uint32
	tasks := []func(context.Context) error{
		func(context.Context) error { return errors.New("boom") },
		func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				atomic.AddUint32(&taskCancelled, 1)
			case <-time.After(10 * time.Second):
				c.Error("cancel not received")
			}
			return nil
		},
	}

	err := osutil.RunManyWithContext(ctx, nil, tasks)
	c.Assert(err, check.ErrorMatches, "boom")
	// Second go routine was cancelled
	atomic.LoadUint32(&taskCancelled)
	c.Assert(taskCancelled, check.Equals, uint32(1))
}

func (ctxSuite) TestRunManyCanceled(c *check.C) {
	ctx, cancel := context.WithCancel(context.Background())

	cmds := []*exec.Cmd{
		exec.Command("sleep", "10"),
	}

	var taskCancelled uint32
	tasks := []func(context.Context) error{
		func(context.Context) error {
			select {
			case <-ctx.Done():
				atomic.AddUint32(&taskCancelled, 1)
			case <-time.After(10 * time.Second):
				c.Error("cancel not received")
			}
			return nil
		},
	}

	// Cancel the context shortly after starting
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := osutil.RunManyWithContext(ctx, cmds, tasks)
	// errgroup.WithContext returns context.Canceled when the context is canceled
	c.Assert(err, check.Equals, context.Canceled)
	// The go routine was cancelled
	atomic.LoadUint32(&taskCancelled)
	c.Assert(taskCancelled, check.Equals, uint32(1))
}

func (ctxSuite) TestRunManyEmpty(c *check.C) {
	// Ensure it doesn't hang or crash with nil/empty inputs
	err := osutil.RunManyWithContext(context.Background(), nil, nil)
	c.Assert(err, check.IsNil)
}
