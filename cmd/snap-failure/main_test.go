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

package main_test

import (
	"bytes"
	"testing"

	. "gopkg.in/check.v1"

	failure "github.com/snapcore/snapd/cmd/snap-failure"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type failureSuite struct {
	testutil.BaseTest

	rootdir string

	stderr *bytes.Buffer
	stdout *bytes.Buffer
	log    *bytes.Buffer

	systemctlCmd *testutil.MockCmd
}

func (r *failureSuite) SetUpTest(c *C) {
	r.stderr = bytes.NewBuffer(nil)
	r.stdout = bytes.NewBuffer(nil)

	oldStderr := failure.Stderr
	oldStdout := failure.Stdout
	r.AddCleanup(func() {
		failure.Stderr = oldStderr
		failure.Stdout = oldStdout
	})
	failure.Stderr = r.stderr
	failure.Stdout = r.stdout

	r.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "ubuntu"}))

	r.rootdir = c.MkDir()
	dirs.SetRootDir(r.rootdir)
	r.AddCleanup(func() { dirs.SetRootDir("/") })

	log, restore := logger.MockLogger()
	r.log = log
	r.AddCleanup(func() {
		c.Logf("logs:\n%s", log.String())
		restore()
	})

	r.systemctlCmd = testutil.MockCommand(c, "systemctl", "")
	r.AddCleanup(r.systemctlCmd.Restore)
}

func (r *failureSuite) Stderr() string {
	return r.stderr.String()
}

func (r *failureSuite) Stdout() string {
	return r.stdout.String()
}

var _ = Suite(&failureSuite{})

func (r *failureSuite) TestUnknownArg(c *C) {
	err := failure.ParseArgs([]string{})
	c.Check(err, ErrorMatches, "Please specify the snapd command")
}
