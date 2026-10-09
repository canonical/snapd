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
	"errors"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	snapd_kernel_probe "github.com/snapcore/snapd/cmd/snapd/tool/snapd-kernel-probe"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/testutil"
)

func TestKernelProbe(t *testing.T) {
	TestingT(t)
}

type kernelProbeSuite struct {
	testutil.BaseTest
	fakeroot string
}

var _ = Suite(&kernelProbeSuite{})

func (s *kernelProbeSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.fakeroot = c.MkDir()
	dirs.SetRootDir(s.fakeroot)
	s.AddCleanup(func() { dirs.SetRootDir("") })
	// pretend the current process is the distro snapd binary so that
	// InternalToolPath resolves the helper from the (mocked) distro
	// libexec dir
	s.AddCleanup(snapdtool.MockOsReadlink(func(path string) (string, error) {
		c.Assert(path, Equals, "/proc/self/exe")
		return filepath.Join(dirs.DistroLibExecDir, "snapd"), nil
	}))
}

func (s *kernelProbeSuite) installHelper(c *C) string {
	helper := filepath.Join(dirs.DistroLibExecDir, "snapd-kernel-probe-helper")
	c.Assert(os.MkdirAll(filepath.Dir(helper), 0755), IsNil)
	c.Assert(os.WriteFile(helper, nil, 0755), IsNil)
	return helper
}

func (s *kernelProbeSuite) TestUnknownProbe(c *C) {
	err := snapd_kernel_probe.Run([]string{"frobnicate"})
	c.Check(err, ErrorMatches, `unknown probe "frobnicate"`)
}

func (s *kernelProbeSuite) TestMissingProbeName(c *C) {
	err := snapd_kernel_probe.Run(nil)
	c.Check(err, ErrorMatches, "usage: snapd-kernel-probe <probe> \\[profile\\]")
}

func (s *kernelProbeSuite) TestNetworkBugProbeRequiresProfile(c *C) {
	err := snapd_kernel_probe.Run([]string{"apparmor-5-network-bug"})
	c.Check(err, ErrorMatches, `probe "apparmor-5-network-bug" requires an AppArmor profile name`)
}

func (s *kernelProbeSuite) TestNetworkBugProbeTransitionsAndExecs(c *C) {
	helper := s.installHelper(c)

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

	err := snapd_kernel_probe.Run([]string{"apparmor-5-network-bug", "snapd-kernel-probe-network-bug.123"})
	c.Assert(err, IsNil)

	// the profile transition is requested at exec time, exactly like
	// aa-exec -p does it
	c.Check(string(attrContent), Equals, "exec snapd-kernel-probe-network-bug.123")
	// and the process is replaced by the static helper
	c.Check(execArgv0, Equals, helper)
	c.Check(execArgv, DeepEquals, []string{"snapd-kernel-probe-helper"})
}

func (s *kernelProbeSuite) TestNetworkBugProbeMissingHelper(c *C) {
	// no helper installed in the mocked distro libexec dir
	err := snapd_kernel_probe.Run([]string{"apparmor-5-network-bug", "some-profile"})
	c.Check(err, ErrorMatches, "snapd-kernel-probe-helper is not executable: .*")
}

func (s *kernelProbeSuite) TestNetworkBugProbeAttrWriteFails(c *C) {
	s.installHelper(c)
	restore := snapd_kernel_probe.MockOsWriteFile(func(path string, data []byte, perm os.FileMode) error {
		return errors.New("attr write denied")
	})
	defer restore()

	err := snapd_kernel_probe.Run([]string{"apparmor-5-network-bug", "some-profile"})
	c.Check(err, ErrorMatches, `cannot set AppArmor exec profile "some-profile": attr write denied`)
}
