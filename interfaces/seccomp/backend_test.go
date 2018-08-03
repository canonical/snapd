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
	s.InstallSnap(c, interfaces.ConfinementOptions{}, ifacetest.SambaYamlV1, 0)
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
	s.InstallSnap(c, interfaces.ConfinementOptions{}, ifacetest.HookYaml, 0)
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

	s.InstallSnap(c, interfaces.ConfinementOptions{}, ifacetest.SambaYamlV1, 0)
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
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 0)
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
		// file called "snap.sambda.smbd" was removed
		_, err := os.Stat(profile + ".src")
		c.Check(os.IsNotExist(err), Equals, true)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesHookProfiles(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.HookYaml, 0)
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.foo.hook.configure")

		// Verify that profile "snap.foo.hook.configure" was removed.
		_, err := os.Stat(profile + ".src")
		c.Check(os.IsNotExist(err), Equals, true)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreApps(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 0)
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
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 0)
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
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1WithNmbd, 0)
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
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlWithHook, 0)
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
	restore = seccomp.MockRequiresSocketcall(func() bool { return false })
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

		snapInfo := s.InstallSnap(c, scenario.opts, ifacetest.SambaYamlV1, 0)
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
	restore = seccomp.MockRequiresSocketcall(func() bool { return false })
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

	s.InstallSnap(c, interfaces.ConfinementOptions{}, snapYaml, 0)
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
	restore := seccomp.MockRequiresSocketcall(func() bool { return true })
	defer restore()

	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlV1, nil)
	// NOTE: we don't call seccomp.MockTemplate()
	err := s.Backend.Setup(snapInfo, interfaces.ConfinementOptions{}, s.Repo)
	c.Assert(err, IsNil)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	c.Assert(profile+".src", testutil.FileContains, "\nsocketcall\n")
}

func (s *backendSuite) TestSocketcallIsNotAddedWhenNotRequired(c *C) {
	restore := seccomp.MockRequiresSocketcall(func() bool { return false })
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

	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{DevMode: true}, ifacetest.SambaYamlV1, 0)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	c.Assert(profile+".src", Not(testutil.FileContains), "# complain mode logging unavailable\n")
	s.RemoveSnap(c, snapInfo)

	snapInfo = s.InstallSnap(c, interfaces.ConfinementOptions{DevMode: false}, ifacetest.SambaYamlV1, 0)
	profile = filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	c.Assert(profile+".src", Not(testutil.FileContains), "# complain mode logging unavailable\n")
	s.RemoveSnap(c, snapInfo)

	snapInfo = s.InstallSnap(c, interfaces.ConfinementOptions{Classic: true}, ClassicYamlV1, 0)
	profile = filepath.Join(dirs.SnapSeccompDir, "snap.test-classic.sh")
	c.Assert(profile+".src", Not(testutil.FileContains), "# complain mode logging unavailable\n")
	s.RemoveSnap(c, snapInfo)
}

func (s *backendSuite) TestSystemKeyRetLogUnsupported(c *C) {
	restore := release.MockSecCompActions([]string{"allow", "errno", "kill", "trace", "trap"})
	defer restore()

	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{DevMode: true}, ifacetest.SambaYamlV1, 0)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	c.Assert(profile+".src", testutil.FileContains, "# complain mode logging unavailable\n")
	s.RemoveSnap(c, snapInfo)

	snapInfo = s.InstallSnap(c, interfaces.ConfinementOptions{DevMode: false}, ifacetest.SambaYamlV1, 0)
	profile = filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	c.Assert(profile+".src", Not(testutil.FileContains), "# complain mode logging unavailable\n")
	s.RemoveSnap(c, snapInfo)

	snapInfo = s.InstallSnap(c, interfaces.ConfinementOptions{Classic: true}, ClassicYamlV1, 0)
	profile = filepath.Join(dirs.SnapSeccompDir, "snap.test-classic.sh")
	c.Assert(profile+".src", Not(testutil.FileContains), "# complain mode logging unavailable\n")
	s.RemoveSnap(c, snapInfo)
}

func (s *backendSuite) TestSandboxFeatures(c *C) {
	restore := seccomp.MockKernelFeatures(func() []string { return []string{"foo", "bar"} })
	defer restore()
	restore = seccomp.MockRequiresSocketcall(func() bool { return true })
	defer restore()

	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{"kernel:foo", "kernel:bar", "bpf-argument-filtering", "require-socketcall"})

	restore = seccomp.MockRequiresSocketcall(func() bool { return false })
	defer restore()

	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{"kernel:foo", "kernel:bar", "bpf-argument-filtering"})
}

func (s *backendSuite) TestRequiresSocketcallByNotNeededArch(c *C) {
	testArchs := []string{"amd64", "armhf", "arm64", "unknownDefault"}
	for _, arch := range testArchs {
		restore := seccomp.MockUbuntuKernelArchitecture(func() string { return arch })
		defer restore()
		c.Assert(seccomp.RequiresSocketcall(), Equals, false)
	}
}

func (s *backendSuite) TestRequiresSocketcallForceByArch(c *C) {
	testArchs := []string{"powerpc", "ppc64el", "s390x"}
	for _, arch := range testArchs {
		restore := seccomp.MockUbuntuKernelArchitecture(func() string { return arch })
		defer restore()
		c.Assert(seccomp.RequiresSocketcall(), Equals, true)
	}
}

func (s *backendSuite) TestRequiresSocketcallForcedViaUbuntuRelease(c *C) {
	testDistros := []string{"ubuntu", "other"}
	testArchs := []string{"i386", "other"}
	testReleases := []string{"14.04", "16.04", "other"}
	for _, distro := range testDistros {
		restore := seccomp.MockReleaseInfoId(distro)
		defer restore()
		for _, arch := range testArchs {
			restore := seccomp.MockUbuntuKernelArchitecture(func() string { return arch })
			defer restore()
			for _, release := range testReleases {
				restore = seccomp.MockReleaseInfoVersionId(release)
				defer restore()

				expected := true
				if distro == "other" || arch == "other" || release == "other" {
					expected = false
				}
				c.Assert(seccomp.RequiresSocketcall(), Equals, expected)
			}
		}
	}
}

func (s *backendSuite) TestRequiresSocketcallForcedViaKernelVersion(c *C) {
	restore := seccomp.MockReleaseInfoVersionId("debian")
	defer restore()

	testArchs := []string{"i386", "other"}
	testVersions := []string{"4.2", "4.3", "4.4"}
	for _, arch := range testArchs {
		restore = seccomp.MockUbuntuKernelArchitecture(func() string { return arch })
		defer restore()
		for _, version := range testVersions {
			restore = seccomp.MockKernelVersion(func() string { return version })
			defer restore()

			expected := false
			if arch == "i386" && version == "4.2" {
				expected = true
			}
			c.Assert(seccomp.RequiresSocketcall(), Equals, expected)
		}
	}
}
