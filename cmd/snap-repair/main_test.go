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

package main_test

import (
	"bytes"
	"testing"

	. "gopkg.in/check.v1"

	repair "github.com/snapcore/snapd/cmd/snap-repair"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type repairSuite struct {
	testutil.BaseTest
	baseRunnerSuite

	rootdir string

	stdout *bytes.Buffer
	stderr *bytes.Buffer

	restore func()
}

func (r *repairSuite) SetUpSuite(c *C) {
	r.baseRunnerSuite.SetUpSuite(c)
	r.restore = httputil.SetUserAgentFromVersion("", "")
}

func (r *repairSuite) TearDownSuite(c *C) {
	r.restore()
}

func (r *repairSuite) SetUpTest(c *C) {
	r.BaseTest.SetUpTest(c)
	r.baseRunnerSuite.SetUpTest(c)

	r.stdout = bytes.NewBuffer(nil)
	r.stderr = bytes.NewBuffer(nil)

	oldStdout := repair.Stdout
	r.AddCleanup(func() { repair.Stdout = oldStdout })
	repair.Stdout = r.stdout

	oldStderr := repair.Stderr
	r.AddCleanup(func() { repair.Stderr = oldStderr })
	repair.Stderr = r.stderr

	r.rootdir = c.MkDir()
	dirs.SetRootDir(r.rootdir)
	r.AddCleanup(func() { dirs.SetRootDir("/") })
}

func (r *repairSuite) Stdout() string {
	return r.stdout.String()
}

func (r *repairSuite) Stderr() string {
	return r.stderr.String()
}

var _ = Suite(&repairSuite{})

func (r *repairSuite) TestUnknownArg(c *C) {
	err := repair.ParseArgs([]string{})
	c.Check(err, ErrorMatches, "Please specify one command of: list, run or show")
}

func (r *repairSuite) TestRunOnClassic(c *C) {
	defer release.MockOnClassic(true)()

	err := repair.Run()
	c.Check(err, ErrorMatches, "cannot use snap-repair on a classic system")
}
