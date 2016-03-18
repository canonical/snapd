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

package apparmor_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/interfaces/apparmor"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/testutil"
)

type cfgSuite struct {
	cfg      interfaces.SecurityBackend
	snapInfo *snap.Info
	appInfo  *snap.AppInfo
}

var _ = Suite(&cfgSuite{
	cfg: &apparmor.Configurator{},
})

func (s *cfgSuite) SetUpTest(c *C) {
	snapInfo, err := snap.InfoFromSnapYaml([]byte(`
name: SNAP
version: VERSION
developer: DEVELOPER
apps:
    APP:
`))
	c.Assert(err, IsNil)
	s.snapInfo = snapInfo
	s.appInfo = snapInfo.Apps["APP"]
}

// Tests for Configurator.SecuritySystem()

func (s *cfgSuite) TestSecuritySystem(c *C) {
	c.Assert(s.cfg.SecuritySystem(), Equals, interfaces.SecurityAppArmor)
}

// Tests for Configurator.DirStateForInstalledSnap()

func (s *cfgSuite) TestDirStateForInstalledSnap(c *C) {
	// NOTE: replace the real template with a shorter variant
	restore := apparmor.MockTemplate("\n" +
		"###VAR###\n" +
		"###PROFILEATTACH### (attach_disconnected) {\n" +
		"}\n")
	defer restore()
	for _, scenario := range []struct {
		developerMode bool
		snippets      map[string][][]byte
		content       map[string]*osutil.FileState
	}{
		// no snippets, no just the default template
		{
			content: map[string]*osutil.FileState{
				"snap.SNAP.APP": {
					Mode: 0644,
					Content: []byte(`
@{APP_APPNAME}="APP"
@{APP_ID_DBUS}="SNAP_2eDEVELOPER_5fAPP_5fVERSION"
@{APP_PKGNAME_DBUS}="SNAP_2eDEVELOPER"
@{APP_PKGNAME}="SNAP.DEVELOPER"
@{APP_VERSION}="VERSION"
@{INSTALL_DIR}="{/snaps,/gadget}"
profile "snap.SNAP.APP" (attach_disconnected) {
}
`),
				},
			},
		},
		{
			snippets: map[string][][]byte{
				"APP": {[]byte("snippet1"), []byte("snippet2")},
			},
			content: map[string]*osutil.FileState{
				"snap.SNAP.APP": {
					Mode: 0644,
					Content: []byte(`
@{APP_APPNAME}="APP"
@{APP_ID_DBUS}="SNAP_2eDEVELOPER_5fAPP_5fVERSION"
@{APP_PKGNAME_DBUS}="SNAP_2eDEVELOPER"
@{APP_PKGNAME}="SNAP.DEVELOPER"
@{APP_VERSION}="VERSION"
@{INSTALL_DIR}="{/snaps,/gadget}"
profile "snap.SNAP.APP" (attach_disconnected) {
snippet1
snippet2
}
`),
				},
			},
		},
		{
			developerMode: true,
			content: map[string]*osutil.FileState{
				"snap.SNAP.APP": {
					Mode: 0644,
					Content: []byte(`
@{APP_APPNAME}="APP"
@{APP_ID_DBUS}="SNAP_2eDEVELOPER_5fAPP_5fVERSION"
@{APP_PKGNAME_DBUS}="SNAP_2eDEVELOPER"
@{APP_PKGNAME}="SNAP.DEVELOPER"
@{APP_VERSION}="VERSION"
@{INSTALL_DIR}="{/snaps,/gadget}"
profile "snap.SNAP.APP" (attach_disconnected,complain) {
}
`),
				},
			},
		},
		{
			developerMode: true,
			snippets: map[string][][]byte{
				"APP": {[]byte("snippet1"), []byte("snippet2")},
			},
			content: map[string]*osutil.FileState{
				"snap.SNAP.APP": {
					Mode: 0644,
					Content: []byte(`
@{APP_APPNAME}="APP"
@{APP_ID_DBUS}="SNAP_2eDEVELOPER_5fAPP_5fVERSION"
@{APP_PKGNAME_DBUS}="SNAP_2eDEVELOPER"
@{APP_PKGNAME}="SNAP.DEVELOPER"
@{APP_VERSION}="VERSION"
@{INSTALL_DIR}="{/snaps,/gadget}"
profile "snap.SNAP.APP" (attach_disconnected,complain) {
snippet1
snippet2
}
`),
				},
			},
		},
	} {
		dir, glob, content, err := s.cfg.DirStateForInstalledSnap(
			s.snapInfo, scenario.developerMode, scenario.snippets)
		c.Assert(err, IsNil)
		c.Check(dir, Equals, apparmor.Directory())
		c.Check(glob, Equals, "snap.SNAP.*")
		c.Check(content, DeepEquals, scenario.content)
		// Sanity checking as required by osutil.EnsureDirState()
		for name := range content {
			// Ensure that the file name matches the returned glob.
			matched, err := filepath.Match(glob, name)
			c.Assert(err, IsNil)
			c.Check(matched, Equals, true)
			// Ensure that the file name has no directory component
			c.Check(filepath.Base(name), Equals, name)
		}
	}
}

func (s *cfgSuite) TestRealDefaultTemplateIsNormallyUsed(c *C) {
	// NOTE: we don't call apparmor.MockTemplate()
	_, _, content, err := s.cfg.DirStateForInstalledSnap(s.snapInfo, false, nil)
	c.Assert(err, IsNil)
	profile := string(content["snap.SNAP.APP"].Content)
	for _, line := range []string{
		// NOTE: a few randomly picked lines from the real profile.  Comments
		// and empty lines are avoided as those can be discarded in the future.
		"#include <tunables/global>\n",
		"/tmp/   r,\n",
		"/sys/class/ r,\n",
	} {
		c.Assert(profile, testutil.Contains, line)
	}
}

// Tests for Configurator.DirStateForRemovedSnap()

func (s *cfgSuite) TestDirStateForRemovedSnap(c *C) {
	dir, glob := s.cfg.DirStateForRemovedSnap(s.snapInfo)
	c.Check(dir, Equals, apparmor.Directory())
	c.Check(glob, Equals, "snap.SNAP.*")
}

// Tests for ProfileFile, ProfileName and Directory

func (s *cfgSuite) TestProfileName(c *C) {
	c.Assert(apparmor.ProfileName(s.appInfo), Equals, "snap.SNAP.APP")
}

func (s *cfgSuite) TestProfileFile(c *C) {
	c.Assert(apparmor.ProfileFile(s.appInfo), Equals, "snap.SNAP.APP")
}

func (s *cfgSuite) TestDirectory(c *C) {
	c.Assert(apparmor.Directory(), Equals, "/var/lib/snappy/apparmor/profiles")
}

// Tests for ConfigureSnapSecurity and Finalize

type endToEndSuite struct {
	cfg     interfaces.SecurityBackend
	repo    *interfaces.Repository
	iface   interfaces.Interface
	rootDir string
	cmds    map[string]*testutil.MockCmd
}

var _ = Suite(&endToEndSuite{
	iface: &interfaces.TestInterface{
		InterfaceName: "iface",
		SlotSnippetCallback: func(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
			return []byte("plug snippet"), nil
		},
		PlugSnippetCallback: func(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
			return []byte("plug snippet"), nil
		},
		PermanentPlugSnippetCallback: func(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
			return []byte("permanent plug snippet"), nil
		},
		PermanentSlotSnippetCallback: func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
			return []byte("permanent slot snippet"), nil
		},
	},
})

func (s *endToEndSuite) SetUpTest(c *C) {
	// NOTE: Each test needs a separate configurator
	s.cfg = &apparmor.Configurator{}
	// Isolate this test to a temporary directory
	s.rootDir = c.MkDir()
	dirs.SetRootDir(s.rootDir)
	// Mock away any real apparmor interaction
	s.cmds = apparmor.MockExternalCommands(c)
	// Prepare a directory for apparmor profiles.
	// NOTE: Normally this is a part of the OS snap.
	err := os.MkdirAll(apparmor.Directory(), 0700)
	c.Assert(err, IsNil)
	// Create a fresh repository for each test
	s.repo = interfaces.NewRepository()
	err = s.repo.AddInterface(s.iface)
	c.Assert(err, IsNil)
}

func (s *endToEndSuite) TearDownTest(c *C) {
	for _, cmd := range s.cmds {
		cmd.Restore()
	}
	dirs.SetRootDir("/")
}

// installSnap "installs" a snap from YAML.
func (s *endToEndSuite) installSnap(c *C, developerMode bool, snapYaml string) *snap.Info {
	snapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	s.addPlugsSlots(c, snapInfo)
	s.configureSecurity(c, snapInfo, developerMode)
	return snapInfo
}

// installSnap "updates" an existing snap from YAML.
func (s *endToEndSuite) updateSnap(c *C, oldSnapInfo *snap.Info, developerMode bool, snapYaml string) *snap.Info {
	newSnapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	c.Assert(newSnapInfo.Name, Equals, oldSnapInfo.Name)
	s.removePlugsSlots(c, oldSnapInfo)
	s.addPlugsSlots(c, newSnapInfo)
	s.configureSecurity(c, newSnapInfo, developerMode)
	return newSnapInfo
}

// removeSnap "removes" an "installed" snap.
func (s *endToEndSuite) removeSnap(c *C, snapInfo *snap.Info) {
	s.removePlugsSlots(c, snapInfo)
	s.deconfigureSecurity(c, snapInfo)
}

func (s *endToEndSuite) addPlugsSlots(c *C, snapInfo *snap.Info) {
	for _, plugInfo := range snapInfo.Plugs {
		plug := &interfaces.Plug{PlugInfo: plugInfo}
		err := s.repo.AddPlug(plug)
		c.Assert(err, IsNil)
		c.Logf("added plug: %s", plug)
	}
	for _, slotInfo := range snapInfo.Slots {
		slot := &interfaces.Slot{SlotInfo: slotInfo}
		err := s.repo.AddSlot(slot)
		c.Assert(err, IsNil)
		c.Logf("added slot: %s", slot)
	}
}

func (s *endToEndSuite) removePlugsSlots(c *C, snapInfo *snap.Info) {
	for _, plug := range s.repo.Plugs(snapInfo.Name) {
		err := s.repo.RemovePlug(plug.Snap.Name, plug.Name)
		c.Assert(err, IsNil)
		c.Logf("removed plug: %s", plug)
	}
	for _, slot := range s.repo.Slots(snapInfo.Name) {
		err := s.repo.RemoveSlot(slot.Snap.Name, slot.Name)
		c.Assert(err, IsNil)
		c.Logf("removed slot: %s", slot)
	}
}

func (s *endToEndSuite) configureSecurity(c *C, snapInfo *snap.Info, developerMode bool) {
	err := s.cfg.ConfigureSnapSecurity(s.repo, snapInfo, developerMode)
	c.Assert(err, IsNil)
	err = s.cfg.Finalize()
	c.Assert(err, IsNil)
}

func (s *endToEndSuite) deconfigureSecurity(c *C, snapInfo *snap.Info) {
	err := s.cfg.DeconfigureSnapSecurity(snapInfo)
	c.Assert(err, IsNil)
	err = s.cfg.Finalize()
	c.Assert(err, IsNil)
}

func (s *endToEndSuite) TestInstallingSnapWritesAndLoadsProfiles(c *C) {
	developerMode := false
	s.installSnap(c, developerMode, `
name: samba
version: version
developer: acme
apps:
    smbd:
`)
	aaProfile := filepath.Join(apparmor.Directory(), "snap.samba.smbd")
	// file called "snap.sambda.smbd" was created
	_, err := os.Stat(aaProfile)
	c.Check(err, IsNil)
	// apparmor_parser was was used to load that file
	c.Check(s.cmds["apparmor_parser"].Calls(), DeepEquals, []string{
		fmt.Sprintf("--replace --write-cache -O no-expr-simplify --cache-loc=/var/cache/apparmor %s", aaProfile),
	})
}

func (s *endToEndSuite) TestSecurityIsStable(c *C) {
	const yaml = `
name: samba
version: version
developer: acme
apps:
    smbd:
`
	for _, developerMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, developerMode, yaml)
		s.cmds["apparmor_parser"].ForgetCalls()
		s.configureSecurity(c, snapInfo, developerMode)
		// profiles are not re-compiled or re-loaded when nothing changes
		c.Check(s.cmds["apparmor_parser"].Calls(), HasLen, 0)
	}
}

func (s *endToEndSuite) TestRemovingSnapRemovesAndUnloadsProfiles(c *C) {
	const yaml = `
name: samba
version: 1
developer: acme
apps:
    smbd:
`
	for _, developerMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, developerMode, yaml)
		s.cmds["apparmor_parser"].ForgetCalls()
		s.removeSnap(c, snapInfo)
		aaProfile := filepath.Join(apparmor.Directory(), "snap.samba.smbd")
		// file called "snap.sambda.smbd" was removed
		_, err := os.Stat(aaProfile)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor_parser was was used to unload the profile
		c.Check(s.cmds["apparmor_parser"].Calls(), DeepEquals, []string{
			"--remove snap.samba.smbd",
		})
	}
}

func (s *endToEndSuite) TestUpdatingSnapMakesNeccesaryChanges(c *C) {
	const before = `
name: samba
version: 1
developer: acme
apps:
    smbd:
`
	const after = `
name: samba
version: 2
developer: acme
apps:
    smbd:
`
	for _, developerMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, developerMode, before)
		s.cmds["apparmor_parser"].ForgetCalls()
		snapInfo = s.updateSnap(c, snapInfo, developerMode, after)
		aaProfile := filepath.Join(apparmor.Directory(), "snap.samba.smbd")
		// apparmor_parser was used to reload the profile because snap version is
		// inside the generated policy.
		c.Check(s.cmds["apparmor_parser"].Calls(), DeepEquals, []string{
			fmt.Sprintf("--replace --write-cache -O no-expr-simplify --cache-loc=/var/cache/apparmor %s", aaProfile),
		})
	}
}

func (s *endToEndSuite) TestUpdatingSnapMakesToOneWithMoreApps(c *C) {
	const before = `
name: samba
version: 1
developer: acme
apps:
    smbd:
`
	// NOTE: the version is the same so that no unrelated changes are made
	const after = `
name: samba
version: 1
developer: acme
apps:
    smbd:
    nmbd:
`
	for _, developerMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, developerMode, before)
		s.cmds["apparmor_parser"].ForgetCalls()
		snapInfo = s.updateSnap(c, snapInfo, developerMode, after)
		aaProfile := filepath.Join(apparmor.Directory(), "snap.samba.nmbd")
		// file called "snap.sambda.nmbd" was created
		_, err := os.Stat(aaProfile)
		c.Check(err, IsNil)
		// apparmor_parser was used to load the new profile
		c.Check(s.cmds["apparmor_parser"].Calls(), DeepEquals, []string{
			fmt.Sprintf("--replace --write-cache -O no-expr-simplify --cache-loc=/var/cache/apparmor %s", aaProfile),
		})
	}
}

func (s *endToEndSuite) TestUpdatingSnapMakesToOneWithFewerApps(c *C) {
	const before = `
name: samba
version: 1
developer: acme
apps:
    smbd:
    nmbd:
`
	// NOTE: the version is the same so that no unrelated changes are made
	const after = `
name: samba
version: 1
developer: acme
apps:
    smbd:
`
	for _, developerMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, developerMode, before)
		s.cmds["apparmor_parser"].ForgetCalls()
		snapInfo = s.updateSnap(c, snapInfo, developerMode, after)
		aaProfile := filepath.Join(apparmor.Directory(), "snap.samba.nmbd")
		// file called "snap.sambda.nmbd" was removed
		_, err := os.Stat(aaProfile)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor_parser was used to remove the unused profile
		c.Check(s.cmds["apparmor_parser"].Calls(), DeepEquals, []string{"--remove snap.samba.nmbd"})
	}
}
