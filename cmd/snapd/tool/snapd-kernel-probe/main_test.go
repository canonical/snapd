// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package snapd_kernel_probe_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	. "gopkg.in/check.v1"

	snapd_kernel_probe "github.com/snapcore/snapd/cmd/snapd/tool/snapd-kernel-probe"
	"github.com/snapcore/snapd/testutil"
)

func TestKernelProbe(t *testing.T) {
	TestingT(t)
}

type kernelProbeSuite struct {
	testutil.BaseTest
}

var _ = Suite(&kernelProbeSuite{})

func (s *kernelProbeSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.AddCleanup(snapd_kernel_probe.MockRaiseLoopback(func() error { return nil }))
}

func (s *kernelProbeSuite) TestUnknownProbe(c *C) {
	_, err := snapd_kernel_probe.Run([]string{"frobnicate"})
	c.Check(err, ErrorMatches, `unknown probe "frobnicate"`)
}

func (s *kernelProbeSuite) TestMissingProbeName(c *C) {
	_, err := snapd_kernel_probe.Run(nil)
	c.Check(err, ErrorMatches, "usage: snapd-kernel-probe <probe> \\[profile\\]")
}

func (s *kernelProbeSuite) TestNetworkBugProbeRequiresProfile(c *C) {
	_, err := snapd_kernel_probe.Run([]string{"apparmor-5-network-bug"})
	c.Check(err, ErrorMatches, `probe "apparmor-5-network-bug" requires an AppArmor profile name`)
}

func (s *kernelProbeSuite) TestNetworkBugProbeTransitionsAndExecs(c *C) {
	var attrContent []byte
	attrPath := filepath.Join(c.MkDir(), "attr-exec")
	restore := snapd_kernel_probe.MockProcThreadSelfAttrExec(attrPath)
	defer restore()
	restore = snapd_kernel_probe.MockOsWriteFile(func(path string, data []byte, perm os.FileMode) error {
		c.Check(path, Equals, attrPath)
		attrContent = data
		return nil
	})
	defer restore()

	var execArgv0 string
	var execArgv []string
	restore = snapd_kernel_probe.MockSyscallExec(func(argv0 string, argv []string, envv []string) error {
		execArgv0 = argv0
		execArgv = argv
		return nil
	})
	defer restore()

	_, err := snapd_kernel_probe.Run([]string{"apparmor-5-network-bug", "snapd-kernel-probe-network-bug.123"})
	c.Assert(err, IsNil)

	// the profile transition is requested at exec time, exactly like
	// aa-exec -p does it
	c.Check(string(attrContent), Equals, "exec snapd-kernel-probe-network-bug.123")
	// and the process is replaced by snapd running the confined probe
	c.Check(execArgv0, Equals, "/proc/self/exe")
	c.Check(execArgv, DeepEquals, []string{"snapd", "snapd-kernel-probe", "apparmor-5-network-bug-confined"})
}

func (s *kernelProbeSuite) TestNetworkBugProbeAttrWriteFails(c *C) {
	restore := snapd_kernel_probe.MockOsWriteFile(func(path string, data []byte, perm os.FileMode) error {
		return errors.New("attr write denied")
	})
	defer restore()

	_, err := snapd_kernel_probe.Run([]string{"apparmor-5-network-bug", "some-profile"})
	c.Check(err, ErrorMatches, `cannot set AppArmor exec profile "some-profile": attr write denied`)
}

func (s *kernelProbeSuite) TestNetworkBugProbeRaisesLoopbackBeforeTransition(c *C) {
	var calls []string
	restore := snapd_kernel_probe.MockRaiseLoopback(func() error {
		calls = append(calls, "loopback")
		return nil
	})
	defer restore()
	restore = snapd_kernel_probe.MockOsWriteFile(func(path string, data []byte, perm os.FileMode) error {
		calls = append(calls, "attr")
		return nil
	})
	defer restore()
	restore = snapd_kernel_probe.MockSyscallExec(func(argv0 string, argv []string, envv []string) error {
		calls = append(calls, "exec")
		return nil
	})
	defer restore()

	_, err := snapd_kernel_probe.Run([]string{"apparmor-5-network-bug", "some-profile"})
	c.Assert(err, IsNil)
	c.Check(calls, DeepEquals, []string{"loopback", "attr", "exec"})
}

func (s *kernelProbeSuite) TestNetworkBugProbeLoopbackFailureAborts(c *C) {
	restore := snapd_kernel_probe.MockRaiseLoopback(func() error {
		return errors.New("cannot bring up loopback: boom")
	})
	defer restore()
	restore = snapd_kernel_probe.MockOsWriteFile(func(path string, data []byte, perm os.FileMode) error {
		c.Error("attr/exec must not be written when loopback setup fails")
		return nil
	})
	defer restore()
	restore = snapd_kernel_probe.MockSyscallExec(func(argv0 string, argv []string, envv []string) error {
		c.Error("probe must not be executed when loopback setup fails")
		return nil
	})
	defer restore()

	_, err := snapd_kernel_probe.Run([]string{"apparmor-5-network-bug", "some-profile"})
	c.Check(err, ErrorMatches, "cannot bring up loopback: boom")
}

func (s *kernelProbeSuite) TestReportFailureErrno(c *C) {
	var buf bytes.Buffer
	status := snapd_kernel_probe.ReportFailure(&buf, "write", &os.SyscallError{Syscall: "write", Err: syscall.EACCES})
	c.Check(status, Equals, 13)
	c.Check(buf.String(), Equals, "FAIL write 13\n")
}

func (s *kernelProbeSuite) TestReportFailureNoErrno(c *C) {
	var buf bytes.Buffer
	status := snapd_kernel_probe.ReportFailure(&buf, "connect", errors.New("boom"))
	c.Check(status, Equals, 125)
	c.Check(buf.String(), Equals, "error: cannot connect: boom\n")
}

func (s *kernelProbeSuite) TestConfinedProbeUnconfined(c *C) {
	// Without confinement the exchange must succeed; this needs a working
	// IPv4 loopback in the test environment.
	var buf bytes.Buffer
	status := snapd_kernel_probe.RunConfined(&buf)
	if status != 0 {
		c.Skip("loopback exchange unavailable: " + buf.String())
	}
	c.Check(buf.String(), Equals, "")
}

func (s *kernelProbeSuite) TestEnsureOwnNetns(c *C) {
	dir := c.MkDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	c.Assert(os.WriteFile(a, nil, 0644), IsNil)
	c.Assert(os.WriteFile(b, nil, 0644), IsNil)
	// a hard link has the same device and inode, like two references
	// to the same namespace
	alias := filepath.Join(dir, "alias")
	c.Assert(os.Link(a, alias), IsNil)

	c.Check(snapd_kernel_probe.EnsureOwnNetns(a, b), IsNil)
	c.Check(snapd_kernel_probe.EnsureOwnNetns(a, alias), ErrorMatches,
		"refusing to configure loopback in the network namespace of the parent process")
	c.Check(snapd_kernel_probe.EnsureOwnNetns(filepath.Join(dir, "missing"), b), ErrorMatches,
		"cannot inspect own network namespace: .*")
	c.Check(snapd_kernel_probe.EnsureOwnNetns(a, filepath.Join(dir, "missing")), ErrorMatches,
		"cannot inspect parent network namespace: .*")
}
