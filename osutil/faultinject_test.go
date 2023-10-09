// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build faultinject

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type testhelperFaultInjectionSuite struct {
	testutil.BaseTest

	sysroot string
}

var _ = Suite(&testhelperFaultInjectionSuite{})

func (s *testhelperFaultInjectionSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.sysroot = c.MkDir()
	restore := osutil.MockInjectSysroot(s.sysroot)
	s.AddCleanup(restore)
	oldSnappyTesting := os.Getenv("SNAPPY_TESTING")
	s.AddCleanup(func() { os.Setenv("SNAPPY_TESTING", oldSnappyTesting) })
	s.AddCleanup(func() { os.Unsetenv("SNAPD_FAULT_INJECT") })
}

func (s *testhelperFaultInjectionSuite) TestFaultInject(c *C) {
	foreverLoopCalls := 0
	restore := osutil.MockForeverLoop(func() {
		foreverLoopCalls++
	})
	defer restore()
	os.Setenv("SNAPPY_TESTING", "1")
	stderrBuf := &bytes.Buffer{}
	restore = osutil.MockStderr(stderrBuf)
	defer restore()

	sysrqFile := filepath.Join(s.sysroot, "/proc/sysrq-trigger")
	c.Assert(os.MkdirAll(filepath.Dir(sysrqFile), 0755), IsNil)

	os.Setenv("SNAPD_FAULT_INJECT", "tag:reboot,othertag:panic,funtag:reboot")

	c.Assert(os.WriteFile(sysrqFile, nil, 0644), IsNil)
	osutil.MaybeInjectFault("tag")
	c.Assert(filepath.Join(s.sysroot, "/proc/sysrq-trigger"), testutil.FileEquals, "b\n")
	c.Check(foreverLoopCalls, Equals, 1)
	c.Check(filepath.Join(s.sysroot, "/var/lib/snapd/faults/tag:reboot"), testutil.FilePresent)
	c.Check(stderrBuf.String(), Equals, "injecting \"reboot\" fault for tag \"tag\"\n")
	// trying to inject a tag again does nothing as long as the stamp file is present
	c.Assert(os.WriteFile(sysrqFile, nil, 0644), IsNil)
	stderrBuf.Reset()
	osutil.MaybeInjectFault("tag")
	c.Check(foreverLoopCalls, Equals, 1)
	c.Check(stderrBuf.String(), Equals, "")

	// remove the tag now
	c.Assert(os.Remove(filepath.Join(s.sysroot, "/var/lib/snapd/faults/tag:reboot")), IsNil)
	osutil.MaybeInjectFault("tag")
	// and the fault was injected
	c.Check(filepath.Join(s.sysroot, "/var/lib/snapd/faults/tag:reboot"), testutil.FilePresent)
	c.Check(foreverLoopCalls, Equals, 2)
	c.Check(stderrBuf.String(), Equals, "injecting \"reboot\" fault for tag \"tag\"\n")

	// try another tag that triggers reboot
	c.Assert(os.WriteFile(sysrqFile, nil, 0644), IsNil)
	stderrBuf.Reset()
	osutil.MaybeInjectFault("funtag")
	c.Assert(filepath.Join(s.sysroot, "/proc/sysrq-trigger"), testutil.FileEquals, "b\n")
	c.Check(foreverLoopCalls, Equals, 3)
	c.Check(stderrBuf.String(), Equals, "injecting \"reboot\" fault for tag \"funtag\"\n")
	c.Check(filepath.Join(s.sysroot, "/var/lib/snapd/faults/funtag:reboot"), testutil.FilePresent)

	// clear sysrq-trigger file
	c.Assert(os.WriteFile(sysrqFile, nil, 0644), IsNil)
	stderrBuf.Reset()
	c.Assert(func() {
		osutil.MaybeInjectFault("othertag")
	}, PanicMatches, `fault "othertag:panic"`)
	c.Check(stderrBuf.String(), Equals, "injecting \"panic\" fault for tag \"othertag\"\n")
	// we have a stamp file
	c.Check(filepath.Join(s.sysroot, "/var/lib/snapd/faults/othertag:panic"), testutil.FilePresent)
	// nothing was written to the sysrq file
	c.Assert(filepath.Join(s.sysroot, "/proc/sysrq-trigger"), testutil.FileEquals, "")
	c.Check(foreverLoopCalls, Equals, 3)

	// nothing happens until the stamp file is present
	stderrBuf.Reset()
	osutil.MaybeInjectFault("othertag")
	c.Check(stderrBuf.String(), Equals, "")
	// remove it
	c.Check(os.Remove(filepath.Join(s.sysroot, "/var/lib/snapd/faults/othertag:panic")), IsNil)
	// and the fault can be triggered
	c.Assert(func() {
		osutil.MaybeInjectFault("othertag")
	}, PanicMatches, `fault "othertag:panic"`)
	c.Check(stderrBuf.String(), Equals, "injecting \"panic\" fault for tag \"othertag\"\n")

	// now a tag that is not set
	osutil.MaybeInjectFault("unset")
	c.Assert(filepath.Join(s.sysroot, "/proc/sysrq-trigger"), testutil.FileEquals, "")
	osutil.MaybeInjectFault("otherunsertag")
	c.Assert(filepath.Join(s.sysroot, "/proc/sysrq-trigger"), testutil.FileEquals, "")
	c.Check(foreverLoopCalls, Equals, 3)
}

func (s *testhelperFaultInjectionSuite) TestFaultInjectDisabledNoSnappyTesting(c *C) {
	// with SNAPPY_TESTING disabled, fault injection is disabled as well
	c.Assert(os.Unsetenv("SNAPPY_TESTING"), IsNil)

	restore := osutil.MockForeverLoop(func() {
		c.Fatalf("unexpected call")
	})
	defer restore()
	sysrqFile := filepath.Join(s.sysroot, "/proc/sysrq-trigger")
	os.Setenv("SNAPD_FAULT_INJECT", "tag:reboot,othertag:panic")

	osutil.MaybeInjectFault("tag")
	c.Check(sysrqFile, testutil.FileAbsent)
	c.Check(filepath.Join(s.sysroot, "/var/lib/snapd/faults/tag:reboot"), testutil.FileAbsent)

	osutil.MaybeInjectFault("othertag")
	c.Check(sysrqFile, testutil.FileAbsent)
	c.Check(filepath.Join(s.sysroot, "/var/lib/snapd/faults/othertag:panic"), testutil.FileAbsent)
}

func (s *testhelperFaultInjectionSuite) TestFaultInjectDisabledNoTags(c *C) {
	os.Setenv("SNAPPY_TESTING", "1")
	// no fault injection tags
	os.Setenv("SNAPD_FAULT_INJECT", "")

	restore := osutil.MockForeverLoop(func() {
		c.Fatalf("unexpected call")
	})
	defer restore()

	// and nothing happens
	osutil.MaybeInjectFault("tag")
	osutil.MaybeInjectFault("othertag")
}

func (s *testhelperFaultInjectionSuite) TestFaultInjectInvalidTags(c *C) {
	os.Setenv("SNAPPY_TESTING", "1")
	// no fault injection tags
	os.Setenv("SNAPD_FAULT_INJECT", "tag:panic,bad/tag:reboot")

	restore := osutil.MockForeverLoop(func() {
		c.Fatalf("unexpected call")
	})
	defer restore()

	stderrBuf := &bytes.Buffer{}
	restore = osutil.MockStderr(stderrBuf)
	defer restore()

	// SNAPD_FAULT_INJECT is invalid
	osutil.MaybeInjectFault("tag")
	c.Check(stderrBuf.String(), Equals, "invalid fault tags \"tag:panic,bad/tag:reboot\"\n")

	stderrBuf.Reset()
	// invalid tag
	os.Setenv("SNAPD_FAULT_INJECT", "tag::bad,othertag:reboot")

	osutil.MaybeInjectFault("tag")
	c.Check(stderrBuf.String(), Equals, "incorrect fault tag: \"tag::bad\"\n")

	stderrBuf.Reset()
	// another invalid tag
	os.Setenv("SNAPD_FAULT_INJECT", "tag,othertag:reboot")

	osutil.MaybeInjectFault("tag")
	c.Check(stderrBuf.String(), Equals, "")
}
