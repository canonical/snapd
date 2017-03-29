// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"regexp"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type backendSuite struct {
	ifacetest.BackendSuite
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
}

func (s *backendSuite) TearDownTest(c *C) {
	s.BackendSuite.TearDownTest(c)
}

// Tests for Setup() and Remove()
func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, interfaces.SecuritySecComp)
}

func (s *backendSuite) TestInstallingSnapWritesProfiles(c *C) {
	s.InstallSnap(c, interfaces.ConfinementOptions{}, ifacetest.SambaYamlV1, 0)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	// file called "snap.sambda.smbd" was created
	_, err := os.Stat(profile)
	c.Check(err, IsNil)
}

func (s *backendSuite) TestInstallingSnapWritesHookProfiles(c *C) {
	s.InstallSnap(c, interfaces.ConfinementOptions{}, ifacetest.HookYaml, 0)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.foo.hook.configure")

	// Verify that profile named "snap.foo.hook.configure" was created.
	_, err := os.Stat(profile)
	c.Check(err, IsNil)
}

func (s *backendSuite) TestRemovingSnapRemovesProfiles(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 0)
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
		// file called "snap.sambda.smbd" was removed
		_, err := os.Stat(profile)
		c.Check(os.IsNotExist(err), Equals, true)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesHookProfiles(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.HookYaml, 0)
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.foo.hook.configure")

		// Verify that profile "snap.foo.hook.configure" was removed.
		_, err := os.Stat(profile)
		c.Check(os.IsNotExist(err), Equals, true)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreApps(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1WithNmbd, 0)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.nmbd")
		_, err := os.Stat(profile)
		// file called "snap.sambda.nmbd" was created
		c.Check(err, IsNil)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithHooks(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlWithHook, 0)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.hook.configure")

		_, err := os.Stat(profile)
		// Verify that profile "snap.samba.hook.configure" was created.
		c.Check(err, IsNil)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerApps(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1WithNmbd, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.nmbd")
		// file called "snap.sambda.nmbd" was removed
		_, err := os.Stat(profile)
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
		_, err := os.Stat(profile)
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
	data, err := ioutil.ReadFile(profile)
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

func (s *backendSuite) TestRealDefaultHookTemplateIsNormallyUsed(c *C) {
	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlWithHook, nil)
	// NOTE: we don't call seccomp.MockTemplate()
	err := s.Backend.Setup(snapInfo, interfaces.ConfinementOptions{}, s.Repo)
	c.Assert(err, IsNil)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.hook.configure")
	data, err := ioutil.ReadFile(profile)
	c.Assert(err, IsNil)
	for _, line := range []string{
		"\nbind\n",
	} {
		c.Assert(string(data), testutil.Contains, line)
	}
}

func (s *backendSuite) TestRealDefaultTemplateDoesNotHaveDefaultHookTemplate(c *C) {
	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlV1, nil)
	// NOTE: we don't call seccomp.MockTemplate()
	err := s.Backend.Setup(snapInfo, interfaces.ConfinementOptions{}, s.Repo)
	c.Assert(err, IsNil)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
	data, err := ioutil.ReadFile(profile)
	c.Assert(err, IsNil)
	for _, line := range []string{
		"# Needed to workaround LP #1674193; when snapctl is executed inside a",
	} {
		match, _ := regexp.MatchString(line, string(data))
		c.Assert(match, Equals, false)
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
	// NOTE: replace the real template with a shorter variant
	restore := seccomp.MockTemplate([]byte("default\n"))
	defer restore()
	for _, scenario := range combineSnippetsScenarios {
		s.Iface.SecCompPermanentSlotCallback = func(spec *seccomp.Specification, slot *interfaces.Slot) error {
			if scenario.snippet != "" {
				spec.AddSnippet(scenario.snippet)
			}
			return nil
		}

		snapInfo := s.InstallSnap(c, scenario.opts, ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapSeccompDir, "snap.samba.smbd")
		data, err := ioutil.ReadFile(profile)
		c.Assert(err, IsNil)
		c.Check(string(data), Equals, scenario.content)
		stat, err := os.Stat(profile)
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
	// NOTE: replace the real template with a shorter variant
	restore := seccomp.MockTemplate([]byte("default\n"))
	defer restore()

	iface2 := &ifacetest.TestInterface{InterfaceName: "iface2"}
	s.Repo.AddInterface(iface2)

	s.Iface.SecCompPermanentSlotCallback = func(spec *seccomp.Specification, slot *interfaces.Slot) error {
		spec.AddSnippet("zzz")
		return nil
	}
	iface2.SecCompPermanentSlotCallback = func(spec *seccomp.Specification, slot *interfaces.Slot) error {
		spec.AddSnippet("aaa")
		return nil
	}

	s.InstallSnap(c, interfaces.ConfinementOptions{}, snapYaml, 0)
	profile := filepath.Join(dirs.SnapSeccompDir, "snap.foo.foo")
	data, err := ioutil.ReadFile(profile)
	c.Assert(err, IsNil)
	c.Check(string(data), Equals, "default\naaa\nzzz\n")
	stat, err := os.Stat(profile)
	c.Assert(err, IsNil)
	c.Check(stat.Mode(), Equals, os.FileMode(0644))
}
