// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type testhelperSuite struct{}

var _ = Suite(&testhelperSuite{})

func mockOsArgs(args []string) (restore func()) {
	old := os.Args
	os.Args = args
	return func() {
		os.Args = old
	}
}

func (s *testhelperSuite) TestIsTestBinary(c *C) {
	// obvious case
	c.Assert(osutil.IsTestBinary(), Equals, true)

	defer mockOsArgs([]string{"foo", "bar", "baz"})()
	c.Assert(osutil.IsTestBinary(), Equals, false)
}

func (s *testhelperSuite) TestMustBeTestBinary(c *C) {
	// obvious case
	osutil.MustBeTestBinary("unexpected panic")

	defer mockOsArgs([]string{"foo", "bar", "baz"})()
	c.Assert(func() { osutil.MustBeTestBinary("panic message") }, PanicMatches, "panic message")
}

func (s *testhelperSuite) TestBinaryNoRegressionWithValidApp(c *C) {
	// a snap app named 'test' is valid, we must not be confused here
	defer mockOsArgs([]string{"/snap/bin/some-snap.test", "bar", "baz"})()
	// must not be considered a test binary
	c.Assert(osutil.IsTestBinary(), Equals, false)
	// must panic since binary is a non-test one
	c.Assert(func() { osutil.MustBeTestBinary("non test binary, expecting a panic") },
		PanicMatches, "non test binary, expecting a panic")
}

func (s *testhelperSuite) TestFaultInject(c *C) {
	sysroot := c.MkDir()
	restore := osutil.MockInjectSysroot(sysroot)
	defer restore()
	foreverLoopCalls := 0
	restore = osutil.MockForeverLoop(func() {
		foreverLoopCalls++
	})
	defer restore()
	oldSnappyTesting := os.Getenv("SNAPPY_TESTING")
	defer func() { os.Setenv("SNAPPY_TESTING", oldSnappyTesting) }()
	os.Setenv("SNAPPY_TESTING", "1")
	defer func() { os.Unsetenv("SNAPD_FAULT_INJECT") }()
	stderrBuf := &bytes.Buffer{}
	osutil.MockStderr(stderrBuf)

	sysrqFile := filepath.Join(sysroot, "/proc/sysrq-trigger")
	c.Assert(os.MkdirAll(filepath.Dir(sysrqFile), 0755), IsNil)

	os.Setenv("SNAPD_FAULT_INJECT", "tag:reboot,othertag:panic,funtag:reboot")

	c.Assert(ioutil.WriteFile(sysrqFile, nil, 0644), IsNil)
	osutil.MaybeInjectFault("tag")
	c.Assert(filepath.Join(sysroot, "/proc/sysrq-trigger"), testutil.FileEquals, "b\n")
	c.Check(foreverLoopCalls, Equals, 1)
	c.Check(filepath.Join(sysroot, "/var/lib/snapd/faults/tag:reboot"), testutil.FilePresent)
	c.Check(stderrBuf.String(), Equals, "injecting \"reboot\" fault for tag \"tag\"\n")
	// trying to inject a tag again does nothing as long as the stamp file is present
	c.Assert(ioutil.WriteFile(sysrqFile, nil, 0644), IsNil)
	stderrBuf.Reset()
	osutil.MaybeInjectFault("tag")
	c.Check(foreverLoopCalls, Equals, 1)
	c.Check(stderrBuf.String(), Equals, "")

	// remove the tag now
	c.Assert(os.Remove(filepath.Join(sysroot, "/var/lib/snapd/faults/tag:reboot")), IsNil)
	osutil.MaybeInjectFault("tag")
	// and the fault was injected
	c.Check(filepath.Join(sysroot, "/var/lib/snapd/faults/tag:reboot"), testutil.FilePresent)
	c.Check(foreverLoopCalls, Equals, 2)
	c.Check(stderrBuf.String(), Equals, "injecting \"reboot\" fault for tag \"tag\"\n")

	// try another tag that triggers reboot
	c.Assert(ioutil.WriteFile(sysrqFile, nil, 0644), IsNil)
	stderrBuf.Reset()
	osutil.MaybeInjectFault("funtag")
	c.Assert(filepath.Join(sysroot, "/proc/sysrq-trigger"), testutil.FileEquals, "b\n")
	c.Check(foreverLoopCalls, Equals, 3)
	c.Check(stderrBuf.String(), Equals, "injecting \"reboot\" fault for tag \"funtag\"\n")
	c.Check(filepath.Join(sysroot, "/var/lib/snapd/faults/funtag:reboot"), testutil.FilePresent)

	// clear sysrq-trigger file
	c.Assert(ioutil.WriteFile(sysrqFile, nil, 0644), IsNil)
	stderrBuf.Reset()
	c.Assert(func() {
		osutil.MaybeInjectFault("othertag")
	}, PanicMatches, `fault "othertag:panic"`)
	c.Check(stderrBuf.String(), Equals, "injecting \"panic\" fault for tag \"othertag\"\n")
	// we have a stamp file
	c.Check(filepath.Join(sysroot, "/var/lib/snapd/faults/othertag:panic"), testutil.FilePresent)
	// nothing was written to the sysrq file
	c.Assert(filepath.Join(sysroot, "/proc/sysrq-trigger"), testutil.FileEquals, "")
	c.Check(foreverLoopCalls, Equals, 3)

	// nothing happens until the stamp file is present
	stderrBuf.Reset()
	osutil.MaybeInjectFault("othertag")
	c.Check(stderrBuf.String(), Equals, "")
	// remove it
	c.Check(os.Remove(filepath.Join(sysroot, "/var/lib/snapd/faults/othertag:panic")), IsNil)
	// and the fault can be triggered
	c.Assert(func() {
		osutil.MaybeInjectFault("othertag")
	}, PanicMatches, `fault "othertag:panic"`)
	c.Check(stderrBuf.String(), Equals, "injecting \"panic\" fault for tag \"othertag\"\n")

	// now a tag that is not set
	osutil.MaybeInjectFault("unset")
	c.Assert(filepath.Join(sysroot, "/proc/sysrq-trigger"), testutil.FileEquals, "")
	osutil.MaybeInjectFault("otherunsertag")
	c.Assert(filepath.Join(sysroot, "/proc/sysrq-trigger"), testutil.FileEquals, "")
	c.Check(foreverLoopCalls, Equals, 3)
}
