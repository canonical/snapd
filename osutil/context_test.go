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
	"io"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/net/context"
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
	ctx, _ := context.WithTimeout(context.Background(), time.Second/100)
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
	ctx, _ := context.WithTimeout(context.Background(), time.Second/100)
	// check we can copy if we're quick
	n, err := io.Copy(osutil.ContextWriter(ctx), strings.NewReader("hello"))
	c.Check(err, check.IsNil)
	c.Check(n, check.Equals, int64(len("hello")))
}

func (ctxSuite) TestRun(c *check.C) {
	ctx, _ := context.WithTimeout(context.Background(), time.Second/100)
	cmd := exec.Command("/bin/sleep", "1")
	err := osutil.RunWithContext(ctx, cmd)
	c.Check(err, check.ErrorMatches, "signal: killed")
}

func (ctxSuite) TestRunDone(c *check.C) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := exec.Command("/bin/sleep", "1")
	err := osutil.RunWithContext(ctx, cmd)
	c.Check(err, check.Equals, context.Canceled)
}

func (ctxSuite) TestRunSuccess(c *check.C) {
	ctx, _ := context.WithTimeout(context.Background(), time.Second)
	cmd := exec.Command("/bin/sleep", "0.01")
	err := osutil.RunWithContext(ctx, cmd)
	c.Check(err, check.IsNil)
}

func (ctxSuite) TestRunSuccessfulFailure(c *check.C) {
	ctx, _ := context.WithTimeout(context.Background(), time.Second)
	cmd := exec.Command("not/something/you/can/run")
	err := osutil.RunWithContext(ctx, cmd)
	c.Check(err, check.ErrorMatches, `fork/exec \S+: no such file or directory`)
}
