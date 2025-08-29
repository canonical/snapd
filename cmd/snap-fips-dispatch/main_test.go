// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

// A dispatcher for bootstrapping FIPS environment. It is expected to be
// symlinked as /usr/bin/snap,
// /usr/lib/snapd/{snapd,snap-repair,snap-bootstrap}.
//
// The dispatcher sets up the environment by expliclty enabling FIPS support
// (through GOFIPS=1), and injects environment variables such that the Go FIPS
// toolchain runtime can locate the relevant OpenSSL FIPS provider module.
package main_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-fips-dispatch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&dispatchSuite{})

type dispatchSuite struct {
	testutil.BaseTest

	logbuf *bytes.Buffer
}

func (s *dispatchSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	os.Setenv("SNAPD_DEBUG", "1")
	s.AddCleanup(func() { os.Unsetenv("SNAPD_DEBUG") })

	buf, restore := logger.MockLogger()
	s.AddCleanup(restore)
	s.logbuf = buf

	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "ubuntu"}))

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.AddCleanup(main.MockSnapdtoolDispatchWithFIPS(func(target string) error {
		panic("unmocked call")
	}))
}

func (s *dispatchSuite) testDispatchTo(c *C, args []string, to string) {
	var targetBin string
	defer main.MockSnapdtoolDispatchWithFIPS(func(target string) error {
		targetBin = target
		panic("execution in target")
	})()

	c.Assert(func() { main.Run(args) }, PanicMatches, "execution in target")
	c.Assert(targetBin, Equals, to)
}

func (s *dispatchSuite) TestSnapd(c *C) {
	s.testDispatchTo(c, []string{"/usr/lib/snapd/snapd"}, "/usr/lib/snapd/snapd-fips")
}

func (s *dispatchSuite) TestSnap(c *C) {
	s.testDispatchTo(c, []string{"/usr/bin/snap"}, "/usr/bin/snap-fips")
}

func (s *dispatchSuite) TestSnapRepair(c *C) {
	s.testDispatchTo(c, []string{"/usr/lib/snapd/snap-repair"}, "/usr/lib/snapd/snap-repair-fips")
}

func (s *dispatchSuite) TestSnapBootstrap(c *C) {
	s.testDispatchTo(c, []string{"/usr/lib/snapd/snap-bootstrap"}, "/usr/lib/snapd/snap-bootstrap-fips")
}

func (s *dispatchSuite) TestSnapExplcitRunApp(c *C) {
	s.testDispatchTo(c, []string{"/usr/bin/snap", "run", "foo"}, "/usr/bin/snap-fips")
}

func (s *dispatchSuite) TestSnapImplicitRunApp(c *C) {
	c.Assert(os.MkdirAll(dirs.SnapBinariesDir, 0o755), IsNil)
	c.Assert(os.Symlink("this-is-magic-symlink", filepath.Join(dirs.SnapBinariesDir, "foo")), IsNil)
	s.testDispatchTo(c, []string{"/snap/bin/foo", "bar"}, "/usr/bin/snap-fips")
}
