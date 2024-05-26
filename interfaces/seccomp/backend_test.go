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
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/osutil"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	seccomp_sandbox "github.com/snapcore/snapd/sandbox/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type backendSuite struct {
	ifacetest.BackendSuite

	snapSeccomp   *testutil.MockCmd
	profileHeader string
	meas          *timings.Span

	restoreReadlink func()
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
	mylog.

		// Prepare a directory for seccomp profiles.
		// NOTE: Normally this is a part of the OS snap.
		Check(os.MkdirAll(dirs.SnapSeccompDir, 0700))


	s.restoreReadlink = snapdtool.MockOsReadlink(func(string) (string, error) {
		// pretend that snapd is run from distro libexecdir
		return filepath.Join(dirs.DistroLibExecDir, "snapd"), nil
	})
	snapSeccompPath := filepath.Join(dirs.DistroLibExecDir, "snap-seccomp")
	s.snapSeccomp = testutil.MockLockedCommand(c, snapSeccompPath, `
if [ "$1" = "version-info" ]; then
    echo "abcdef 1.2.3 1234abcd -"
fi`)

	s.Backend.Initialize(nil)
	s.profileHeader = `# snap-seccomp version information:
# abcdef 1.2.3 1234abcd -
`
	// make sure initialize called version-info
	c.Check(s.snapSeccomp.Calls(), DeepEquals, [][]string{
		{"snap-seccomp", "version-info"},
	})
	s.snapSeccomp.ForgetCalls()

	perf := timings.New(nil)
	s.meas = perf.StartSpan("", "")
}

func (s *backendSuite) TearDownTest(c *C) {
	s.BackendSuite.TearDownTest(c)

	s.snapSeccomp.Restore()
	s.restoreReadlink()
}

func (s *backendSuite) TestInitialize(c *C) {
	mylog.Check(s.Backend.Initialize(nil))

	fname := filepath.Join(dirs.SnapSeccompDir, "global.bin")
	if arch.Endian() == binary.BigEndian {
		c.Check(fname, testutil.FileEquals, seccomp.GlobalProfileBE)
	} else {
		c.Check(fname, testutil.FileEquals, seccomp.GlobalProfileLE)
	}
}

// Tests for Setup() and Remove()
func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, interfaces.SecuritySecComp)
}

func (s *backendSuite) TestInstallingSnapWritesProfiles(c *C) {
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.SambaYamlV1, 0)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	// file called "snap.sambda.smbd" was created
	_ := mylog.Check2(os.Stat(profile + ".src"))
	c.Check(err, IsNil)
	// and got compiled
	c.Check(s.snapSeccomp.Calls(), DeepEquals, [][]string{
		{"snap-seccomp", "compile", profile + ".src", profile + ".bin2"},
	})
}

func (s *backendSuite) TestInstallingSnapWritesHookProfiles(c *C) {
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.HookYaml, 0)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.foo.hook.configure")

	// Verify that profile named "snap.foo.hook.configure" was created.
	_ := mylog.Check2(os.Stat(profile + ".src"))
	c.Check(err, IsNil)
	// and got compiled
	c.Check(s.snapSeccomp.Calls(), DeepEquals, [][]string{
		{"snap-seccomp", "compile", profile + ".src", profile + ".bin2"},
	})
}

func (s *backendSuite) TestInstallingComponentWritesHookProfiles(c *C) {
	const instanceName = ""
	s.testInstallingComponentWritesHookProfiles(c, instanceName)
}

func (s *backendSuite) TestInstallingComponentWritesHookProfilesInstance(c *C) {
	const instanceName = "snap_instance"
	s.testInstallingComponentWritesHookProfiles(c, instanceName)
}

func (s *backendSuite) testInstallingComponentWritesHookProfiles(c *C, instanceName string) {
	testedConfinementOpts := []interfaces.ConfinementOptions{
		{},
	}

	for _, opts := range testedConfinementOpts {
		info := s.InstallSnapWithComponents(c, opts, instanceName, ifacetest.SnapWithComponentsYaml, 0, []string{ifacetest.ComponentYaml})

		expectedName := info.InstanceName()

		componentHookProfile := filepath.Join(dirs.SnapSeccompDir, fmt.Sprintf("snap.%s+comp.hook.install", expectedName))
		appProfile := filepath.Join(dirs.SnapSeccompDir, fmt.Sprintf("snap.%s.app", expectedName))

		// verify that profiles were created
		c.Check(componentHookProfile+".src", testutil.FilePresent)
		c.Check(appProfile+".src", testutil.FilePresent)

		// and got compiled
		c.Check(s.snapSeccomp.Calls(), testutil.DeepContains, []string{
			"snap-seccomp", "compile", componentHookProfile + ".src", componentHookProfile + ".bin2",
		})

		s.RemoveSnap(c, info)
		s.snapSeccomp.ForgetCalls()
	}
}

func (s *backendSuite) TestInstallingSnapWritesProfilesWithReexec(c *C) {
	restore := snapdtool.MockOsReadlink(func(string) (string, error) {
		// simulate that we run snapd from core
		return filepath.Join(dirs.SnapMountDir, "core/42/usr/lib/snapd/snapd"), nil
	})
	defer restore()

	// ensure we have a mocked snap-seccomp on core
	snapSeccompOnCorePath := filepath.Join(dirs.SnapMountDir, "core/42/usr/lib/snapd/snap-seccomp")
	snapSeccompOnCore := testutil.MockLockedCommand(c, snapSeccompOnCorePath, `if [ "$1" = "version-info" ]; then
echo "2345cdef 2.3.4 2345cdef -"
fi`)
	defer snapSeccompOnCore.Restore()
	mylog.

		// rerun initialization
		Check(s.Backend.Initialize(nil))


	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.SambaYamlV1, 0)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	// file called "snap.sambda.smbd" was created
	_ = mylog.Check2(os.Stat(profile + ".src"))
	c.Check(err, IsNil)
	// ensure the snap-seccomp from the regular path was *not* used
	c.Check(s.snapSeccomp.Calls(), HasLen, 0)
	// ensure the snap-seccomp from the core snap was used instead
	c.Check(snapSeccompOnCore.Calls(), DeepEquals, [][]string{
		{"snap-seccomp", "version-info"}, // from Initialize()
		{"snap-seccomp", "compile", profile + ".src", profile + ".bin2"},
	})
	raw := mylog.Check2(os.ReadFile(profile + ".src"))

	c.Assert(bytes.HasPrefix(raw, []byte(`# snap-seccomp version information:
# 2345cdef 2.3.4 2345cdef -
`)), Equals, true)
}

func (s *backendSuite) TestRemovingSnapRemovesProfiles(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
		// file called "snap.sambda.smbd" was removed
		_ := mylog.Check2(os.Stat(profile + ".src"))
		c.Check(os.IsNotExist(err), Equals, true)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesHookProfiles(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.HookYaml, 0)
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.foo.hook.configure")

		// Verify that profile "snap.foo.hook.configure" was removed.
		_ := mylog.Check2(os.Stat(profile + ".src"))
		c.Check(os.IsNotExist(err), Equals, true)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesComponentProfiles(c *C) {
	const instanceName = ""
	s.testRemovingSnapRemovesComponentProfiles(c, instanceName)
}

func (s *backendSuite) TestRemovingSnapRemovesComponentProfilesInstance(c *C) {
	const instanceName = "snap_instance"
	s.testRemovingSnapRemovesComponentProfiles(c, instanceName)
}

func (s *backendSuite) testRemovingSnapRemovesComponentProfiles(c *C, instanceName string) {
	for _, opts := range testedConfinementOpts {
		info := s.InstallSnapWithComponents(c, opts, instanceName, ifacetest.SnapWithComponentsYaml, 0, []string{ifacetest.ComponentYaml})
		s.RemoveSnap(c, info)

		expectedName := info.InstanceName()
		profile := filepath.Join(dirs.SnapSeccompDir, fmt.Sprintf("snap.%s+comp.hook.install", expectedName))
		c.Check(profile+".src", testutil.FileAbsent)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreApps(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1WithNmbd, 0)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.nmbd")
		_ := mylog.Check2(os.Stat(profile + ".src"))
		// file called "snap.sambda.nmbd" was created
		c.Check(err, IsNil)
		// and got compiled
		c.Check(s.snapSeccomp.Calls(), testutil.DeepContains, []string{"snap-seccomp", "compile", profile + ".src", profile + ".bin2"})
		s.snapSeccomp.ForgetCalls()

		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithHooks(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlWithHook, 0)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.hook.configure")

		_ := mylog.Check2(os.Stat(profile + ".src"))
		// Verify that profile "snap.samba.hook.configure" was created.
		c.Check(err, IsNil)
		// and got compiled
		c.Check(s.snapSeccomp.Calls(), testutil.DeepContains, []string{"snap-seccomp", "compile", profile + ".src", profile + ".bin2"})
		s.snapSeccomp.ForgetCalls()

		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreComponents(c *C) {
	const instanceName = ""
	s.testUpdatingSnapToOneWithMoreComponents(c, instanceName)
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreComponentsInstance(c *C) {
	const instanceName = "snap_instance"
	s.testUpdatingSnapToOneWithMoreComponents(c, instanceName)
}

func (s *backendSuite) testUpdatingSnapToOneWithMoreComponents(c *C, instanceName string) {
	for _, opts := range testedConfinementOpts {
		info := s.InstallSnap(c, opts, instanceName, ifacetest.SnapWithComponentsYaml, 0)
		info = s.UpdateSnapWithComponents(c, info, opts, ifacetest.SnapWithComponentsYaml, 0, []string{ifacetest.ComponentYaml})

		expectedName := info.InstanceName()

		profile := filepath.Join(dirs.SnapSeccompDir, fmt.Sprintf("snap.%s+comp.hook.install", expectedName))
		c.Check(profile+".src", testutil.FilePresent)

		// and got compiled
		c.Check(s.snapSeccomp.Calls(), testutil.DeepContains, []string{"snap-seccomp", "compile", profile + ".src", profile + ".bin2"})
		s.snapSeccomp.ForgetCalls()

		s.RemoveSnap(c, info)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerComponents(c *C) {
	const instanceName = ""
	s.testUpdatingSnapToOneWithFewerComponents(c, instanceName)
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerComponentsInstance(c *C) {
	const instanceName = "snap_instance"
	s.testUpdatingSnapToOneWithFewerComponents(c, instanceName)
}

func (s *backendSuite) testUpdatingSnapToOneWithFewerComponents(c *C, instanceName string) {
	for _, opts := range testedConfinementOpts {
		info := s.InstallSnapWithComponents(c, opts, instanceName, ifacetest.SnapWithComponentsYaml, 0, []string{ifacetest.ComponentYaml})
		info = s.UpdateSnapWithComponents(c, info, opts, ifacetest.SnapWithComponentsYaml, 0, nil)

		expectedName := info.InstanceName()

		profile := filepath.Join(dirs.SnapSeccompDir, fmt.Sprintf("snap.%s+comp.hook.install", expectedName))
		c.Check(profile+".src", testutil.FileAbsent)

		s.RemoveSnap(c, info)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerApps(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1WithNmbd, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.nmbd")
		// file called "snap.sambda.nmbd" was removed
		_ := mylog.Check2(os.Stat(profile + ".src"))
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
		_ := mylog.Check2(os.Stat(profile + ".src"))
		c.Check(os.IsNotExist(err), Equals, true)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRealDefaultTemplateIsNormallyUsed(c *C) {
	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlV1, nil)
	appSet := mylog.Check2(interfaces.NewSnapAppSet(snapInfo, nil))

	mylog.
		// NOTE: we don't call seccomp.MockTemplate()
		Check(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, s.meas))

	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	data := mylog.Check2(os.ReadFile(profile + ".src"))

	for _, line := range []string{
		// NOTE: a few randomly picked lines from the real profile.  Comments
		// and empty lines are avoided as those can be discarded in the future.
		"# - create_module, init_module, finit_module, delete_module (kernel modules)\n",
		"open\n",
		"getuid\n",
		"setresuid\n", // this is not random
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
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = seccomp_sandbox.MockActions([]string{"log"})
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
		c.Check(profile+".src", testutil.FileEquals, s.profileHeader+scenario.content)
		stat := mylog.Check2(os.Stat(profile + ".src"))

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
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
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
	c.Check(profile+".src", testutil.FileEquals, s.profileHeader+"default\naaa\nzzz\n")
	stat := mylog.Check2(os.Stat(profile + ".src"))

	c.Check(stat.Mode(), Equals, os.FileMode(0644))
}

func (s *backendSuite) TestBindIsAddedForNonFullApparmorSystems(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Partial)
	defer restore()

	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlV1, nil)
	appSet := mylog.Check2(interfaces.NewSnapAppSet(snapInfo, nil))

	mylog.
		// NOTE: we don't call seccomp.MockTemplate()
		Check(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, s.meas))

	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	c.Assert(profile+".src", testutil.FileContains, "# Add bind() for systems with only Seccomp enabled to workaround\n# LP #1644573\nbind\n")
}

func (s *backendSuite) TestSocketcallIsAddedWhenRequired(c *C) {
	restore := seccomp.MockRequiresSocketcall(func(string) bool { return true })
	defer restore()

	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlV1, nil)
	appSet := mylog.Check2(interfaces.NewSnapAppSet(snapInfo, nil))

	mylog.
		// NOTE: we don't call seccomp.MockTemplate()
		Check(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, s.meas))

	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	c.Assert(profile+".src", testutil.FileContains, "\nsocketcall\n")
}

func (s *backendSuite) TestSocketcallIsNotAddedWhenNotRequired(c *C) {
	restore := seccomp.MockRequiresSocketcall(func(string) bool { return false })
	defer restore()

	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlV1, nil)
	appSet := mylog.Check2(interfaces.NewSnapAppSet(snapInfo, nil))

	mylog.
		// NOTE: we don't call seccomp.MockTemplate()
		Check(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, s.meas))

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
	restore := seccomp_sandbox.MockActions([]string{"allow", "errno", "kill", "log", "trace", "trap"})
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
	restore := seccomp_sandbox.MockActions([]string{"allow", "errno", "kill", "trace", "trap"})
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

	// change version reported by snap-seccomp
	snapSeccomp := testutil.MockLockedCommand(c, filepath.Join(dirs.DistroLibExecDir, "snap-seccomp"), `
if [ "$1" = "version-info" ]; then
    echo "abcdef 1.2.3 1234abcd bpf-actlog"
fi`)
	defer snapSeccomp.Restore()
	mylog.

		// reload cached version info
		Check(s.Backend.Initialize(nil))

	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{"kernel:foo", "kernel:bar", "bpf-argument-filtering", "bpf-actlog"})
}

func (s *backendSuite) TestRequiresSocketcallByNotNeededArch(c *C) {
	testArchs := []string{"amd64", "armhf", "arm64", "powerpc", "ppc64el", "unknownDefault"}
	for _, arch := range testArchs {
		restore := seccomp.MockDpkgKernelArchitecture(func() string { return arch })
		defer restore()
		c.Assert(seccomp.RequiresSocketcall(""), Equals, false)
	}
}

func (s *backendSuite) TestRequiresSocketcallForceByArch(c *C) {
	testArchs := []string{"sparc", "sparc64"}
	for _, arch := range testArchs {
		restore := seccomp.MockDpkgKernelArchitecture(func() string { return arch })
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
		restore = seccomp.MockDpkgKernelArchitecture(func() string { return t.arch })
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
		restore := seccomp.MockDpkgKernelArchitecture(func() string { return t.arch })
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
	restore = seccomp.MockDpkgKernelArchitecture(func() string { return "i386" })
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
	restore = seccomp.MockDpkgKernelArchitecture(func() string { return "i386" })
	defer restore()
	restore = osutil.MockKernelVersion("4.3")
	defer restore()

	testBases := []string{"bare", "core18", "fedora-core"}
	for _, baseSnap := range testBases {
		c.Assert(seccomp.RequiresSocketcall(baseSnap), Equals, false)
	}
}

func (s *backendSuite) TestRebuildsWithVersionInfoWhenNeeded(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = seccomp_sandbox.MockActions([]string{"log"})
	defer restore()
	restore = seccomp.MockRequiresSocketcall(func(string) bool { return false })
	defer restore()

	// NOTE: replace the real template with a shorter variant
	restore = seccomp.MockTemplate([]byte("\ndefault\n"))
	defer restore()

	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")

	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlV1, nil)
	appSet := mylog.Check2(interfaces.NewSnapAppSet(snapInfo, nil))

	mylog.Check(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, s.meas))

	c.Check(profile+".src", testutil.FileEquals, s.profileHeader+"\ndefault\n")

	c.Check(s.snapSeccomp.Calls(), DeepEquals, [][]string{
		{"snap-seccomp", "compile", profile + ".src", profile + ".bin2"},
	})
	mylog.

		// unchanged snap-seccomp version will not trigger a rebuild
		Check(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, s.meas))


	// compilation from this first Setup()
	c.Check(s.snapSeccomp.Calls(), HasLen, 1)

	// change version reported by snap-seccomp
	snapSeccomp := testutil.MockLockedCommand(c, filepath.Join(dirs.DistroLibExecDir, "snap-seccomp"), `
if [ "$1" = "version-info" ]; then
    echo "abcdef 2.3.3 2345abcd -"
fi`)
	defer snapSeccomp.Restore()
	updatedProfileHeader := `# snap-seccomp version information:
# abcdef 2.3.3 2345abcd -
`
	mylog.
		// reload cached version info
		Check(s.Backend.Initialize(nil))


	c.Check(s.snapSeccomp.Calls(), HasLen, 2)
	c.Check(s.snapSeccomp.Calls(), DeepEquals, [][]string{
		// compilation from first Setup()
		{"snap-seccomp", "compile", profile + ".src", profile + ".bin2"},
		// initialization with new version
		{"snap-seccomp", "version-info"},
	})
	mylog.

		// the profile should be rebuilt now
		Check(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, s.meas))

	c.Check(profile+".src", testutil.FileEquals, updatedProfileHeader+"\ndefault\n")

	c.Check(s.snapSeccomp.Calls(), HasLen, 3)
	c.Check(s.snapSeccomp.Calls(), DeepEquals, [][]string{
		// compilation from first Setup()
		{"snap-seccomp", "compile", profile + ".src", profile + ".bin2"},
		// initialization with new version
		{"snap-seccomp", "version-info"},
		// compilation of profiles with new compiler version
		{"snap-seccomp", "compile", profile + ".src", profile + ".bin2"},
	})
}

func (s *backendSuite) TestInitializationDuringBootstrap(c *C) {
	// undo what was done in test set-up
	s.snapSeccomp.Restore()
	os.Remove(s.snapSeccomp.Exe())

	// during bootstrap, before seeding, snapd/core snap is mounted at some
	// random location under /tmp
	tmpDir := c.MkDir()
	restore := snapdtool.MockOsReadlink(func(string) (string, error) {
		return filepath.Join(tmpDir, "usr/lib/snapd/snapd"), nil
	})
	defer restore()

	// ensure we have a mocked snap-seccomp on core
	snapSeccompInMountedPath := filepath.Join(tmpDir, "usr/lib/snapd/snap-seccomp")
	mylog.Check(os.MkdirAll(filepath.Dir(snapSeccompInMountedPath), 0755))

	snapSeccompInMounted := testutil.MockLockedCommand(c, snapSeccompInMountedPath, `if [ "$1" = "version-info" ]; then
echo "2345cdef 2.3.4 2345cdef -"
fi`)
	defer snapSeccompInMounted.Restore()
	mylog.

		// rerun initialization
		Check(s.Backend.Initialize(nil))


	// ensure the snap-seccomp from the regular path was *not* used
	c.Check(s.snapSeccomp.Calls(), HasLen, 0)
	// the one from mounted snap was used
	c.Check(snapSeccompInMounted.Calls(), DeepEquals, [][]string{
		{"snap-seccomp", "version-info"},
	})

	sb, ok := s.Backend.(*seccomp.Backend)
	c.Assert(ok, Equals, true)
	c.Check(sb.VersionInfo(), Equals, seccomp_sandbox.VersionInfo("2345cdef 2.3.4 2345cdef -"))
}

func (s *backendSuite) TestCompilerInitUnhappy(c *C) {
	restore := seccomp.MockSeccompCompilerLookup(func(name string) (string, error) {
		c.Check(name, Equals, "snap-seccomp")
		return "", errors.New("failed")
	})
	defer restore()
	mylog.Check(s.Backend.Initialize(nil))
	c.Assert(err, ErrorMatches, "cannot initialize seccomp profile compiler: failed")
}

func (s *backendSuite) TestSystemUsernamesPolicy(c *C) {
	snapYaml := `
name: app
version: 0.1
system-usernames:
  testid: shared
  testid2: shared
apps:
  cmd:
`
	snapInfo := snaptest.MockInfo(c, snapYaml, nil)
	appSet := mylog.Check2(interfaces.NewSnapAppSet(snapInfo, nil))

	mylog.
		// NOTE: we don't call seccomp.MockTemplate()
		Check(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, s.meas))

	// NOTE: we don't call seccomp.MockTemplate()
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.app.cmd")
	data := mylog.Check2(os.ReadFile(profile + ".src"))

	for _, line := range []string{
		// NOTE: a few randomly picked lines from the real
		// profile.  Comments and empty lines are avoided as
		// those can be discarded in the future.
		"\n# - create_module, init_module, finit_module, delete_module (kernel modules)\n",
		"\nopen\n",
		"\ngetuid\n",
		"\nsetgroups 0 -\n",
		// and a few randomly picked lines from root syscalls
		// with extra \n checks to ensure we have the right
		// "paragraphs" in the generated output
		"\n\n# allow setresgid to root\n",
		"\n# allow setresuid to root\n",
		"\nsetresuid u:root u:root u:root\n",
		// and a few randomly picked lines from global id syscalls
		"\n\n# allow setresgid to testid\n",
		"\n\n# allow setresuid to testid\n",
		"\nsetresuid -1 u:testid -1\n",
		// also for the second user
		"\n\n# allow setresgid to testid2\n",
		"\n# allow setresuid to testid2\n",
		"\nsetresuid -1 u:testid2 -1\n",
	} {
		c.Assert(string(data), testutil.Contains, line)
	}

	// make sure the bare syscalls aren't present
	c.Assert(string(data), Not(testutil.Contains), "setresuid\n")
}

func (s *backendSuite) TestNoSystemUsernamesPolicy(c *C) {
	snapYaml := `
name: app
version: 0.1
apps:
  cmd:
`
	snapInfo := snaptest.MockInfo(c, snapYaml, nil)
	appSet := mylog.Check2(interfaces.NewSnapAppSet(snapInfo, nil))

	mylog.
		// NOTE: we don't call seccomp.MockTemplate()
		Check(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, s.meas))

	// NOTE: we don't call seccomp.MockTemplate()
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.app.cmd")
	data := mylog.Check2(os.ReadFile(profile + ".src"))

	for _, line := range []string{
		// and a few randomly picked lines from root syscalls
		"# allow setresgid to root\n",
		"# allow setresuid to root\n",
		"setresuid u:root u:root u:root\n",
		// and a few randomly picked lines from global id syscalls
		"# allow setresgid to testid\n",
		"# allow setresuid to testid\n",
		"setresuid -1 u:testid -1\n",
	} {
		c.Assert(string(data), Not(testutil.Contains), line)
	}

	// make sure the bare syscalls are present
	c.Assert(string(data), testutil.Contains, "setresuid\n")
}

func (s *backendSuite) TestCleanupWhenOneFailsParallel(c *C) {
	restore := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer restore()
	restore = seccomp_sandbox.MockActions([]string{"log"})
	defer restore()
	restore = seccomp.MockRequiresSocketcall(func(string) bool { return false })
	defer restore()

	// NOTE: replace the real template with a shorter variant
	restore = seccomp.MockTemplate([]byte("\ndefault\n"))
	defer restore()

	snapSeccomp := testutil.MockLockedCommand(c, filepath.Join(dirs.DistroLibExecDir, "snap-seccomp"),
		`
if [ "$1" = "version-info" ]; then
    echo "2345cdef 2.3.4 2345cdef -"
elif [ "$1" = "compile" ] && [ "${2//nmbd}" != "$2" ]; then
    echo "mocked failure"
    exit 1
fi
`)
	defer snapSeccomp.Restore()
	mylog.

		// rerun initialization
		Check(s.Backend.Initialize(nil))


	smbdProfile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	nmbdProfile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.nmbd")

	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlV1WithNmbd, nil)
	appSet := mylog.Check2(interfaces.NewSnapAppSet(snapInfo, nil))

	mylog.Check(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, s.meas))
	c.Assert(err, ErrorMatches, "cannot compile .*nmbd.src: mocked failure")
	for _, profile := range []string{smbdProfile, nmbdProfile} {
		c.Check(profile+".bin", testutil.FileAbsent)
	}

	// 2 compile calls + 1 version-info
	c.Check(snapSeccomp.Calls(), HasLen, 3)
	seen := make(map[string]bool, 2)
	for _, call := range snapSeccomp.Calls() {
		if len(call) == 2 && call[1] == "version-info" {
			continue
		}
		c.Assert(call, HasLen, 4)
		seen[call[2]] = true
	}
	c.Check(seen, DeepEquals, map[string]bool{
		nmbdProfile + ".src": true,
		smbdProfile + ".src": true,
	})
}

type mockedSyncedCompiler struct {
	lock     sync.Mutex
	profiles []string
}

func (m *mockedSyncedCompiler) Compile(in, out string) error {
	m.lock.Lock()
	m.profiles = append(m.profiles, filepath.Base(in))
	m.lock.Unlock()

	f := mylog.Check2(os.Create(out))

	defer f.Close()
	fmt.Fprintf(f, "done %s", filepath.Base(out))
	return nil
}

func (m *mockedSyncedCompiler) VersionInfo() (seccomp_sandbox.VersionInfo, error) {
	return "", nil
}

func (s *backendSuite) TestParallelCompileHappy(c *C) {
	cpus := runtime.NumCPU()

	m := mockedSyncedCompiler{}
	profiles := make([]string, cpus*3)
	for i := range profiles {
		profiles[i] = fmt.Sprintf("profile-%03d", i)
	}
	mylog.Check(seccomp.ParallelCompile(&m, profiles))


	sort.Strings(m.profiles)
	c.Assert(m.profiles, DeepEquals, profiles)

	for _, p := range profiles {
		c.Check(filepath.Join(dirs.SnapSeccompDir, p+".bin2"), testutil.FileEquals, "done "+p+".bin2")
	}
}

type mockedSyncedFailingCompiler struct {
	mockedSyncedCompiler
	whichFail []string
}

func (m *mockedSyncedFailingCompiler) Compile(in, out string) error {
	if b := filepath.Base(out); strutil.ListContains(m.whichFail, b) {
		return fmt.Errorf("failed %v", b)
	}
	return m.mockedSyncedCompiler.Compile(in, out)
}

func (s *backendSuite) TestParallelCompileError(c *C) {
	mylog.Check(os.MkdirAll(dirs.SnapSeccompDir, 0755))

	// 15 profiles
	profiles := make([]string, 15)
	for i := range profiles {
		profiles[i] = fmt.Sprintf("profile-%03d", i)
	}
	m := mockedSyncedFailingCompiler{
		// pretend compilation of those 2 fails
		whichFail: []string{"profile-005.bin2", "profile-009.bin2"},
	}
	mylog.Check(seccomp.ParallelCompile(&m, profiles))
	c.Assert(err, ErrorMatches, "cannot compile .*/bpf/profile-00[59]: failed profile-00[59].bin2")

	// make sure all compiled profiles were removed
	d := mylog.Check2(os.Open(dirs.SnapSeccompDir))

	names := mylog.Check2(d.Readdirnames(-1))

	// only global profile exists
	c.Assert(names, DeepEquals, []string{"global.bin"})
}

func (s *backendSuite) TestParallelCompileRemovesFirst(c *C) {
	mylog.Check(os.MkdirAll(dirs.SnapSeccompDir, 0755))

	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeccompDir, "profile-001.bin2"), nil, 0755))

	mylog.
		// make profiles directory non-accessible
		Check(os.Chmod(dirs.SnapSeccompDir, 0000))

	mylog.Check(os.Chmod(dirs.SnapSeccompDir, 0500))

	defer os.Chmod(dirs.SnapSeccompDir, 0755)

	m := mockedSyncedCompiler{}
	mylog.Check(seccomp.ParallelCompile(&m, []string{"profile-001"}))
	c.Assert(err, ErrorMatches, "remove .*/profile-001.bin2: permission denied")
}
