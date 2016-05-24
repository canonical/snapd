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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type backendSuite struct {
	backend   *apparmor.Backend
	repo      *interfaces.Repository
	iface     *interfaces.TestInterface
	rootDir   string
	parserCmd *testutil.MockCmd
}

var _ = Suite(&backendSuite{
	backend: &apparmor.Backend{},
})

// fakeAppAprmorParser contains shell program that creates fake binary cache entries
// in accordance with what real apparmor_parser would do.
const fakeAppArmorParser = `
cache_dir=""
profile=""
write=""
while [ -n "$1" ]; do
	case "$1" in
		--cache-loc=*)
			cache_dir="$(echo "$1" | cut -d = -f 2)" || exit 1
			;;
		--write-cache)
			write=yes
			;;
		--replace|--remove)
			# Ignore
			;;
		-O)
			# Ignore, discard argument
			shift
			;;
		*)
			profile=$(basename "$1")
			;;
	esac
	shift
done
if [ "$write" = yes ]; then
	echo fake > "$cache_dir/$profile"
fi
`

func (s *backendSuite) SetUpTest(c *C) {
	// Isolate this test to a temporary directory
	s.rootDir = c.MkDir()
	dirs.SetRootDir(s.rootDir)
	// Prepare a directory for apparmor profiles.
	// NOTE: Normally this is a part of the OS snap.
	err := os.MkdirAll(dirs.SnapAppArmorDir, 0700)
	c.Assert(err, IsNil)
	err = os.MkdirAll(dirs.AppArmorCacheDir, 0700)
	c.Assert(err, IsNil)
	// Mock away any real apparmor interaction
	s.parserCmd = testutil.MockCommand(c, "apparmor_parser", fakeAppArmorParser)
	// Create a fresh repository for each test
	s.repo = interfaces.NewRepository()
	s.iface = &interfaces.TestInterface{InterfaceName: "iface"}
	err = s.repo.AddInterface(s.iface)
	c.Assert(err, IsNil)
}

func (s *backendSuite) TearDownTest(c *C) {
	s.parserCmd.Restore()
	dirs.SetRootDir("/")
}

// Tests for Setup() and Remove()
const sambaYaml = `
name: samba
apps:
    smbd:
slots:
    iface:
`
const sambaYamlWithNmbd = `
name: samba
apps:
    smbd:
    nmbd:
slots:
    iface:
`

func (s *backendSuite) TestName(c *C) {
	c.Check(s.backend.Name(), Equals, "apparmor")
}

func (s *backendSuite) TestInstallingSnapWritesAndLoadsProfiles(c *C) {
	devMode := false
	s.installSnap(c, devMode, sambaYaml, 1)
	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
	// file called "snap.sambda.smbd" was created
	_, err := os.Stat(profile)
	c.Check(err, IsNil)
	// apparmor_parser was used to load that file
	c.Check(s.parserCmd.Calls(), DeepEquals, []string{
		fmt.Sprintf("--replace --write-cache -O no-expr-simplify --cache-loc=%s/var/cache/apparmor %s", s.rootDir, profile),
	})
}

func (s *backendSuite) TestProfilesAreAlwaysLoaded(c *C) {
	for _, devMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, devMode, sambaYaml, 1)
		s.parserCmd.ForgetCalls()
		err := s.backend.Setup(snapInfo, devMode, s.repo)
		c.Assert(err, IsNil)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		c.Check(s.parserCmd.Calls(), DeepEquals, []string{
			fmt.Sprintf("--replace --write-cache -O no-expr-simplify --cache-loc=%s/var/cache/apparmor %s", s.rootDir, profile),
		})
		s.removeSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesAndUnloadsProfiles(c *C) {
	for _, devMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, devMode, sambaYaml, 1)
		s.parserCmd.ForgetCalls()
		s.removeSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		// file called "snap.sambda.smbd" was removed
		_, err := os.Stat(profile)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor cache file was removed
		cache := filepath.Join(dirs.AppArmorCacheDir, "snap.samba.smbd")
		_, err = os.Stat(cache)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor_parser was used to unload the profile
		c.Check(s.parserCmd.Calls(), DeepEquals, []string{
			"--remove snap.samba.smbd",
		})
	}
}

func (s *backendSuite) TestUpdatingSnapMakesNeccesaryChanges(c *C) {
	for _, devMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, devMode, sambaYaml, 1)
		s.parserCmd.ForgetCalls()
		snapInfo = s.updateSnap(c, snapInfo, devMode, sambaYaml, 2)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		// apparmor_parser was used to reload the profile because snap revision
		// is inside the generated policy.
		c.Check(s.parserCmd.Calls(), DeepEquals, []string{
			fmt.Sprintf("--replace --write-cache -O no-expr-simplify --cache-loc=%s/var/cache/apparmor %s", s.rootDir, profile),
		})
		s.removeSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreApps(c *C) {
	for _, devMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, devMode, sambaYaml, 1)
		s.parserCmd.ForgetCalls()
		// NOTE: the revision is kept the same to just test on the new application being added
		snapInfo = s.updateSnap(c, snapInfo, devMode, sambaYamlWithNmbd, 1)
		smbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		nmbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.nmbd")
		// file called "snap.sambda.nmbd" was created
		_, err := os.Stat(nmbdProfile)
		c.Check(err, IsNil)
		// apparmor_parser was used to load the both profiles
		c.Check(s.parserCmd.Calls(), DeepEquals, []string{
			fmt.Sprintf("--replace --write-cache -O no-expr-simplify --cache-loc=%s/var/cache/apparmor %s", s.rootDir, nmbdProfile),
			fmt.Sprintf("--replace --write-cache -O no-expr-simplify --cache-loc=%s/var/cache/apparmor %s", s.rootDir, smbdProfile),
		})
		s.removeSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerApps(c *C) {
	for _, devMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, devMode, sambaYamlWithNmbd, 1)
		s.parserCmd.ForgetCalls()
		// NOTE: the revision is kept the same to just test on the application being removed
		snapInfo = s.updateSnap(c, snapInfo, devMode, sambaYaml, 1)
		smbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		nmbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.nmbd")
		// file called "snap.sambda.nmbd" was removed
		_, err := os.Stat(nmbdProfile)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor_parser was used to remove the unused profile
		c.Check(s.parserCmd.Calls(), DeepEquals, []string{
			fmt.Sprintf("--replace --write-cache -O no-expr-simplify --cache-loc=%s/var/cache/apparmor %s", s.rootDir, smbdProfile),
			"--remove snap.samba.nmbd",
		})
		s.removeSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRealDefaultTemplateIsNormallyUsed(c *C) {
	snapInfo, err := snap.InfoFromSnapYaml([]byte(sambaYaml))
	c.Assert(err, IsNil)
	// NOTE: we don't call apparmor.MockTemplate()
	err = s.backend.Setup(snapInfo, false, s.repo)
	c.Assert(err, IsNil)
	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
	data, err := ioutil.ReadFile(profile)
	c.Assert(err, IsNil)
	for _, line := range []string{
		// NOTE: a few randomly picked lines from the real profile.  Comments
		// and empty lines are avoided as those can be discarded in the future.
		"#include <tunables/global>\n",
		"/tmp/   r,\n",
		"/sys/class/ r,\n",
	} {
		c.Assert(string(data), testutil.Contains, line)
	}
}

type combineSnippetsScenario struct {
	devMode bool
	snippet string
	content string
}

const commonPrefix = `
@{APP_NAME}="smbd"
@{SNAP_NAME}="samba"
@{SNAP_REVISION}="1"
@{INSTALL_DIR}="/snap"`

var combineSnippetsScenarios = []combineSnippetsScenario{{
	content: commonPrefix + `
profile "snap.samba.smbd" (attach_disconnected) {

}
`,
}, {
	snippet: "snippet",
	content: commonPrefix + `
profile "snap.samba.smbd" (attach_disconnected) {
snippet
}
`,
}, {
	devMode: true,
	content: commonPrefix + `
profile "snap.samba.smbd" (attach_disconnected,complain) {

}
`,
}, {
	devMode: true,
	snippet: "snippet",
	content: commonPrefix + `
profile "snap.samba.smbd" (attach_disconnected,complain) {
snippet
}
`}}

func (s *backendSuite) TestCombineSnippets(c *C) {
	// NOTE: replace the real template with a shorter variant
	restore := apparmor.MockTemplate([]byte("\n" +
		"###VAR###\n" +
		"###PROFILEATTACH### (attach_disconnected) {\n" +
		"###SNIPPETS###\n" +
		"}\n"))
	defer restore()
	for _, scenario := range combineSnippetsScenarios {
		s.iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
			if scenario.snippet == "" {
				return nil, nil
			}
			return []byte(scenario.snippet), nil
		}
		snapInfo := s.installSnap(c, scenario.devMode, sambaYaml, 1)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		data, err := ioutil.ReadFile(profile)
		c.Assert(err, IsNil)
		c.Check(string(data), Equals, scenario.content)
		stat, err := os.Stat(profile)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.removeSnap(c, snapInfo)
	}
}

// Support code for tests

// installSnap "installs" a snap from YAML.
func (s *backendSuite) installSnap(c *C, devMode bool, snapYaml string, revision int) *snap.Info {
	snapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	snapInfo.Revision = snap.R(revision)
	// this won't come from snap.yaml
	snapInfo.Developer = "acme"
	err = s.repo.AddSnap(snapInfo)
	c.Assert(err, IsNil)
	err = s.backend.Setup(snapInfo, devMode, s.repo)
	c.Assert(err, IsNil)
	return snapInfo
}

// updateSnap "updates" an existing snap from YAML.
func (s *backendSuite) updateSnap(c *C, oldSnapInfo *snap.Info, devMode bool, snapYaml string, revision int) *snap.Info {
	newSnapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	newSnapInfo.Revision = snap.R(revision)
	// this won't come from snap.yaml
	newSnapInfo.Developer = "acme"
	c.Assert(newSnapInfo.Name(), Equals, oldSnapInfo.Name())
	err = s.repo.RemoveSnap(oldSnapInfo.Name())
	c.Assert(err, IsNil)
	err = s.repo.AddSnap(newSnapInfo)
	c.Assert(err, IsNil)
	err = s.backend.Setup(newSnapInfo, devMode, s.repo)
	c.Assert(err, IsNil)
	return newSnapInfo
}

// removeSnap "removes" an "installed" snap.
func (s *backendSuite) removeSnap(c *C, snapInfo *snap.Info) {
	err := s.backend.Remove(snapInfo.Name())
	c.Assert(err, IsNil)
	err = s.repo.RemoveSnap(snapInfo.Name())
	c.Assert(err, IsNil)
}
