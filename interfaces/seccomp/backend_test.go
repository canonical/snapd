// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

package seccomp_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type backendSuite struct {
	ifacetest.BackendSuite

	snapSeccomp *testutil.MockCmd
}

var _ = Suite(&backendSuite{})

var testedConfinementOpts = []interfaces.ConfinementOptions{
	{},
	{DevMode: true},
	{JailMode: true},
	{Classic: true},
}

func (s *backendSuite) SetUpTest(c *C) {
	s.Backend = &seccomp.Backend{}
	s.BackendSuite.SetUpTest(c)
	c.Assert(s.Repo.AddBackend(s.Backend), IsNil)

	// Prepare a directory for seccomp profiles.
	// NOTE: Normally this is a part of the OS snap.
	err := os.MkdirAll(dirs.SnapSeccompDir, 0700)
	c.Assert(err, IsNil)

	snapSeccompPath := filepath.Join(dirs.DistroLibExecDir, "snap-seccomp")
	err = os.MkdirAll(filepath.Dir(snapSeccompPath), 0755)
	c.Assert(err, IsNil)
	s.snapSeccomp = testutil.MockCommand(c, snapSeccompPath, "")
}

func (s *backendSuite) TearDownTest(c *C) {
	s.BackendSuite.TearDownTest(c)

	s.snapSeccomp.Restore()
}

// Tests for Setup() and Remove()
func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, interfaces.SecuritySecComp)
}

func (s *backendSuite) TestInstallingSnapWritesProfiles(c *C) {
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.SambaYamlV1, 0)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	// file called "snap.sambda.smbd" was created
	_, err := os.Stat(profile + ".src")
	c.Check(err, IsNil)
	// and got compiled
	c.Check(s.snapSeccomp.Calls(), DeepEquals, [][]string{
		{"snap-seccomp", "compile", profile + ".src", profile + ".bin"},
	})
}

func (s *backendSuite) TestInstallingSnapWritesHookProfiles(c *C) {
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.HookYaml, 0)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.foo.hook.configure")

	// Verify that profile named "snap.foo.hook.configure" was created.
	_, err := os.Stat(profile + ".src")
	c.Check(err, IsNil)
	// and got compiled
	c.Check(s.snapSeccomp.Calls(), DeepEquals, [][]string{
		{"snap-seccomp", "compile", profile + ".src", profile + ".bin"},
	})
}

func (s *backendSuite) TestInstallingSnapWritesProfilesWithReexec(c *C) {

	restore := seccomp.MockOsReadlink(func(string) (string, error) {
		// simulate that we run snapd from core
		return filepath.Join(dirs.SnapMountDir, "core/42/usr/lib/snapd/snapd"), nil
	})
	defer restore()

	// ensure we have a mocked snap-seccomp on core
	snapSeccompOnCorePath := filepath.Join(dirs.SnapMountDir, "core/42/usr/lib/snapd/snap-seccomp")
	err := os.MkdirAll(filepath.Dir(snapSeccompOnCorePath), 0755)
	c.Assert(err, IsNil)
	snapSeccompOnCore := testutil.MockCommand(c, snapSeccompOnCorePath, "")

	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.SambaYamlV1, 0)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	// file called "snap.sambda.smbd" was created
	_, err = os.Stat(profile + ".src")
	c.Check(err, IsNil)
	// ensure the snap-seccomp from the regular path was *not* used
	c.Check(s.snapSeccomp.Calls(), HasLen, 0)
	// ensure the snap-seccomp from the core snap was used instead
	c.Check(snapSeccompOnCore.Calls(), DeepEquals, [][]string{
		{"snap-seccomp", "compile", profile + ".src", profile + ".bin"},
	})
}

func (s *backendSuite) TestRemovingSnapRemovesProfiles(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
		// file called "snap.sambda.smbd" was removed
		_, err := os.Stat(profile + ".src")
		c.Check(os.IsNotExist(err), Equals, true)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesHookProfiles(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.HookYaml, 0)
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.foo.hook.configure")

		// Verify that profile "snap.foo.hook.configure" was removed.
		_, err := os.Stat(profile + ".src")
		c.Check(os.IsNotExist(err), Equals, true)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreApps(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1WithNmbd, 0)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.nmbd")
		_, err := os.Stat(profile + ".src")
		// file called "snap.sambda.nmbd" was created
		c.Check(err, IsNil)
		// and got compiled
		c.Check(s.snapSeccomp.Calls(), testutil.DeepContains, []string{"snap-seccomp", "compile", profile + ".src", profile + ".bin"})
		s.snapSeccomp.ForgetCalls()

		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithHooks(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlWithHook, 0)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.hook.configure")

		_, err := os.Stat(profile + ".src")
		// Verify that profile "snap.samba.hook.configure" was created.
		c.Check(err, IsNil)
		// and got compiled
		c.Check(s.snapSeccomp.Calls(), testutil.DeepContains, []string{"snap-seccomp", "compile", profile + ".src", profile + ".bin"})
		s.snapSeccomp.ForgetCalls()

		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerApps(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1WithNmbd, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.nmbd")
		// file called "snap.sambda.nmbd" was removed
		_, err := os.Stat(profile + ".src")
		c.Check(os.IsNotExist(err), Equals, true)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithNoHooks(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlWithHook, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.hook.configure")

		// Verify that profile snap.samba.hook.configure was removed.
		_, err := os.Stat(profile + ".src")
		c.Check(os.IsNotExist(err), Equals, true)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRealDefaultTemplateIsNormallyUsed(c *C) {
	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlV1, nil)
	// NOTE: we don't call seccomp.MockTemplate()
	err := s.Backend.Setup(snapInfo, interfaces.ConfinementOptions{}, s.Repo)
	c.Assert(err, IsNil)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	data, err := ioutil.ReadFile(profile + ".src")
	c.Assert(err, IsNil)
	for _, line := range []string{
		// NOTE: a few randomly picked lines from the real profile.  Comments
		// and empty lines are avoided as those can be discarded in the future.
		"# - create_module, init_module, finit_module, delete_module (kernel modules)\n",
		"open\n",
		"getuid\n",
	} {
		c.Assert(string(data), testutil.Contains, line)
	}
}

type combineSnippetsScenario struct {
	opts    interfaces.ConfinementOptions
	snippet string
	content string
}

var combineSnippetsScenarios = []combineSnippetsScenario{{
	opts:    interfaces.ConfinementOptions{},
	content: "default\n",
}, {
	opts:    interfaces.ConfinementOptions{},
	snippet: "snippet",
	content: "default\nsnippet\n",
}, {
	opts:    interfaces.ConfinementOptions{DevMode: true},
	content: "@complain\ndefault\n",
}, {
	opts:    interfaces.ConfinementOptions{DevMode: true},
	snippet: "snippet",
	content: "@complain\ndefault\nsnippet\n",
}, {
	opts:    interfaces.ConfinementOptions{Classic: true},
	snippet: "snippet",
	content: "@unrestricted\ndefault\nsnippet\n",
}, {
	opts:    interfaces.ConfinementOptions{Classic: true, JailMode: true},
	snippet: "snippet",
	content: "default\nsnippet\n",
}}

func (s *backendSuite) TestCombineSnippets(c *C) {
	restore := release.MockForcedDevmode(false)
	defer restore()
	restore = release.MockSecCompActions([]string{"log"})
	defer restore()
	restore = seccomp.MockRequiresSocketcall(func(string) bool { return false })
	defer restore()

	// NOTE: replace the real template with a shorter variant
	restore = seccomp.MockTemplate([]byte("default\n"))
	defer restore()
	for _, scenario := range combineSnippetsScenarios {
		s.Iface.SecCompPermanentSlotCallback = func(spec *seccomp.Specification, slot *snap.SlotInfo) error {
			if scenario.snippet != "" {
				spec.AddSnippet(scenario.snippet)
			}
			return nil
		}

		snapInfo := s.InstallSnap(c, scenario.opts, "", ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
		c.Check(profile+".src", testutil.FileEquals, scenario.content)
		stat, err := os.Stat(profile + ".src")
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}

const snapYaml = `
name: foo
version: 1
developer: acme
apps:
    foo:
        slots: [iface, iface2]
`

// Ensure that combined snippets are sorted
func (s *backendSuite) TestCombineSnippetsOrdering(c *C) {
	restore := release.MockForcedDevmode(false)
	defer restore()
	restore = seccomp.MockRequiresSocketcall(func(string) bool { return false })
	defer restore()

	// NOTE: replace the real template with a shorter variant
	restore = seccomp.MockTemplate([]byte("default\n"))
	defer restore()

	iface2 := &ifacetest.TestInterface{InterfaceName: "iface2"}
	s.Repo.AddInterface(iface2)

	s.Iface.SecCompPermanentSlotCallback = func(spec *seccomp.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("zzz")
		return nil
	}
	iface2.SecCompPermanentSlotCallback = func(spec *seccomp.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("aaa")
		return nil
	}

	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", snapYaml, 0)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.foo.foo")
	c.Check(profile+".src", testutil.FileEquals, "default\naaa\nzzz\n")
	stat, err := os.Stat(profile + ".src")
	c.Assert(err, IsNil)
	c.Check(stat.Mode(), Equals, os.FileMode(0644))
}

func (s *backendSuite) TestBindIsAddedForForcedDevModeSystems(c *C) {
	restore := release.MockForcedDevmode(true)
	defer restore()

	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlV1, nil)
	// NOTE: we don't call seccomp.MockTemplate()
	err := s.Backend.Setup(snapInfo, interfaces.ConfinementOptions{}, s.Repo)
	c.Assert(err, IsNil)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	c.Assert(profile+".src", testutil.FileContains, "\nbind\n")
}

func (s *backendSuite) TestSocketcallIsAddedWhenRequired(c *C) {
	restore := seccomp.MockRequiresSocketcall(func(string) bool { return true })
	defer restore()

	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlV1, nil)
	// NOTE: we don't call seccomp.MockTemplate()
	err := s.Backend.Setup(snapInfo, interfaces.ConfinementOptions{}, s.Repo)
	c.Assert(err, IsNil)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	c.Assert(profile+".src", testutil.FileContains, "\nsocketcall\n")
}

func (s *backendSuite) TestSocketcallIsNotAddedWhenNotRequired(c *C) {
	restore := seccomp.MockRequiresSocketcall(func(string) bool { return false })
	defer restore()

	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlV1, nil)
	// NOTE: we don't call seccomp.MockTemplate()
	err := s.Backend.Setup(snapInfo, interfaces.ConfinementOptions{}, s.Repo)
	c.Assert(err, IsNil)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	c.Assert(profile+".src", Not(testutil.FileContains), "\nsocketcall\n")
}

const ClassicYamlV1 = `
name: test-classic
version: 1
developer: acme
confinement: classic
apps:
  sh:
  `

func (s *backendSuite) TestSystemKeyRetLogSupported(c *C) {
	restore := release.MockSecCompActions([]string{"allow", "errno", "kill", "log", "trace", "trap"})
	defer restore()

	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{DevMode: true}, "", ifacetest.SambaYamlV1, 0)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	c.Assert(profile+".src", Not(testutil.FileContains), "# complain mode logging unavailable\n")
	s.RemoveSnap(c, snapInfo)

	snapInfo = s.InstallSnap(c, interfaces.ConfinementOptions{DevMode: false}, "", ifacetest.SambaYamlV1, 0)
	profile = filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	c.Assert(profile+".src", Not(testutil.FileContains), "# complain mode logging unavailable\n")
	s.RemoveSnap(c, snapInfo)

	snapInfo = s.InstallSnap(c, interfaces.ConfinementOptions{Classic: true}, "", ClassicYamlV1, 0)
	profile = filepath.Join(dirs.SnapSeccompDir, "snap.test-classic.sh")
	c.Assert(profile+".src", Not(testutil.FileContains), "# complain mode logging unavailable\n")
	s.RemoveSnap(c, snapInfo)
}

func (s *backendSuite) TestSystemKeyRetLogUnsupported(c *C) {
	restore := release.MockSecCompActions([]string{"allow", "errno", "kill", "trace", "trap"})
	defer restore()

	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{DevMode: true}, "", ifacetest.SambaYamlV1, 0)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	c.Assert(profile+".src", testutil.FileContains, "# complain mode logging unavailable\n")
	s.RemoveSnap(c, snapInfo)

	snapInfo = s.InstallSnap(c, interfaces.ConfinementOptions{DevMode: false}, "", ifacetest.SambaYamlV1, 0)
	profile = filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	c.Assert(profile+".src", Not(testutil.FileContains), "# complain mode logging unavailable\n")
	s.RemoveSnap(c, snapInfo)

	snapInfo = s.InstallSnap(c, interfaces.ConfinementOptions{Classic: true}, "", ClassicYamlV1, 0)
	profile = filepath.Join(dirs.SnapSeccompDir, "snap.test-classic.sh")
	c.Assert(profile+".src", Not(testutil.FileContains), "# complain mode logging unavailable\n")
	s.RemoveSnap(c, snapInfo)
}

func (s *backendSuite) TestSandboxFeatures(c *C) {
	restore := seccomp.MockKernelFeatures(func() []string { return []string{"foo", "bar"} })
	defer restore()

	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{"kernel:foo", "kernel:bar", "bpf-argument-filtering"})
}

func (s *backendSuite) TestRequiresSocketcallByNotNeededArch(c *C) {
	testArchs := []string{"amd64", "armhf", "arm64", "powerpc", "ppc64el", "unknownDefault"}
	for _, arch := range testArchs {
		restore := seccomp.MockUbuntuKernelArchitecture(func() string { return arch })
		defer restore()
		c.Assert(seccomp.RequiresSocketcall(""), Equals, false)
	}
}

func (s *backendSuite) TestRequiresSocketcallForceByArch(c *C) {
	testArchs := []string{"sparc", "sparc64"}
	for _, arch := range testArchs {
		restore := seccomp.MockUbuntuKernelArchitecture(func() string { return arch })
		defer restore()
		c.Assert(seccomp.RequiresSocketcall(""), Equals, true)
	}
}

func (s *backendSuite) TestRequiresSocketcallForcedViaUbuntuRelease(c *C) {
	// specify "core18" with 4.4 kernel so as not to influence the release
	// check.
	base := "core18"
	restore := osutil.MockKernelVersion("4.4")
	defer restore()

	tests := []struct {
		distro          string
		arch            string
		release         string
		needsSocketcall bool
	}{
		// with core18 as base and 4.4 kernel, we only require
		// socketcall on i386/s390
		{"ubuntu", "i386", "14.04", true},
		{"ubuntu", "s390x", "14.04", true},
		{"ubuntu", "other", "14.04", false},

		// releases after 14.04 aren't forced
		{"ubuntu", "i386", "other", false},
		{"ubuntu", "s390x", "other", false},
		{"ubuntu", "other", "other", false},

		// other distros aren't forced
		{"other", "i386", "14.04", false},
		{"other", "s390x", "14.04", false},
		{"other", "other", "14.04", false},
		{"other", "i386", "other", false},
		{"other", "s390x", "other", false},
		{"other", "other", "other", false},
	}

	for _, t := range tests {
		restore = seccomp.MockReleaseInfoId(t.distro)
		defer restore()
		restore = seccomp.MockUbuntuKernelArchitecture(func() string { return t.arch })
		defer restore()
		restore = seccomp.MockReleaseInfoVersionId(t.release)
		defer restore()

		c.Assert(seccomp.RequiresSocketcall(base), Equals, t.needsSocketcall)
	}
}

func (s *backendSuite) TestRequiresSocketcallForcedViaKernelVersion(c *C) {
	// specify "core18" with non-ubuntu so as not to influence the kernel
	// check.
	base := "core18"

	restore := seccomp.MockReleaseInfoId("other")
	defer restore()

	tests := []struct {
		arch            string
		version         string
		needsSocketcall bool
	}{
		// i386 needs socketcall on <= 4.2 kernels
		{"i386", "4.2", true},
		{"i386", "4.3", false},
		{"i386", "4.4", false},

		// s390x needs socketcall on <= 4.2 kernels
		{"s390x", "4.2", true},
		{"s390x", "4.3", false},
		{"s390x", "4.4", false},

		// other architectures don't require it
		{"other", "4.2", false},
		{"other", "4.3", false},
		{"other", "4.4", false},
	}

	for _, t := range tests {
		restore := seccomp.MockUbuntuKernelArchitecture(func() string { return t.arch })
		defer restore()
		restore = osutil.MockKernelVersion(t.version)
		defer restore()

		// specify "core18" here so as not to influence the
		// kernel check.
		c.Assert(seccomp.RequiresSocketcall(base), Equals, t.needsSocketcall)
	}
}

func (s *backendSuite) TestRequiresSocketcallForcedViaBaseSnap(c *C) {
	// Mock up as non-Ubuntu, i386 with new enough kernel so the base snap
	// check is reached
	restore := seccomp.MockReleaseInfoId("other")
	defer restore()
	restore = seccomp.MockUbuntuKernelArchitecture(func() string { return "i386" })
	defer restore()
	restore = osutil.MockKernelVersion("4.3")
	defer restore()

	testBases := []string{"", "core", "core16"}
	for _, baseSnap := range testBases {
		c.Assert(seccomp.RequiresSocketcall(baseSnap), Equals, true)
	}
}

func (s *backendSuite) TestRequiresSocketcallNotForcedViaBaseSnap(c *C) {
	// Mock up as non-Ubuntu, i386 with new enough kernel so the base snap
	// check is reached
	restore := seccomp.MockReleaseInfoId("other")
	defer restore()
	restore = seccomp.MockUbuntuKernelArchitecture(func() string { return "i386" })
	defer restore()
	restore = osutil.MockKernelVersion("4.3")
	defer restore()

	testBases := []string{"bare", "core18", "fedora-core"}
	for _, baseSnap := range testBases {
		c.Assert(seccomp.RequiresSocketcall(baseSnap), Equals, false)
	}
}
