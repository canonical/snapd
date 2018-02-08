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
	"os/user"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type backendSuite struct {
	ifacetest.BackendSuite

	parserCmd *testutil.MockCmd
}

var _ = Suite(&backendSuite{})

var testedConfinementOpts = []interfaces.ConfinementOptions{
	{},
	{DevMode: true},
	{JailMode: true},
	{Classic: true},
}

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
	s.Backend = &apparmor.Backend{}
	s.BackendSuite.SetUpTest(c)
	c.Assert(s.Repo.AddBackend(s.Backend), IsNil)

	// Prepare a directory for apparmor profiles.
	// NOTE: Normally this is a part of the OS snap.
	err := os.MkdirAll(dirs.SnapAppArmorDir, 0700)
	c.Assert(err, IsNil)
	err = os.MkdirAll(dirs.AppArmorCacheDir, 0700)
	c.Assert(err, IsNil)
	// Mock away any real apparmor interaction
	s.parserCmd = testutil.MockCommand(c, "apparmor_parser", fakeAppArmorParser)
}

func (s *backendSuite) TearDownTest(c *C) {
	s.parserCmd.Restore()

	s.BackendSuite.TearDownTest(c)
}

// Tests for Setup() and Remove()

func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, interfaces.SecurityAppArmor)
}

func (s *backendSuite) TestInstallingSnapWritesAndLoadsProfiles(c *C) {
	s.InstallSnap(c, interfaces.ConfinementOptions{}, ifacetest.SambaYamlV1, 1)
	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
	// file called "snap.sambda.smbd" was created
	_, err := os.Stat(profile)
	c.Check(err, IsNil)
	// apparmor_parser was used to load that file
	c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), "--quiet", profile},
	})
}

func (s *backendSuite) TestInstallingSnapWithHookWritesAndLoadsProfiles(c *C) {
	s.InstallSnap(c, interfaces.ConfinementOptions{}, ifacetest.HookYaml, 1)
	profile := filepath.Join(dirs.SnapAppArmorDir, "snap.foo.hook.configure")

	// Verify that profile "snap.foo.hook.configure" was created
	_, err := os.Stat(profile)
	c.Check(err, IsNil)
	// apparmor_parser was used to load that file
	c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), "--quiet", profile},
	})
}

func (s *backendSuite) TestProfilesAreAlwaysLoaded(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 1)
		s.parserCmd.ForgetCalls()
		err := s.Backend.Setup(snapInfo, opts, s.Repo)
		c.Assert(err, IsNil)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), "--quiet", profile},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesAndUnloadsProfiles(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 1)
		s.parserCmd.ForgetCalls()
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		// file called "snap.sambda.smbd" was removed
		_, err := os.Stat(profile)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor cache file was removed
		cache := filepath.Join(dirs.AppArmorCacheDir, "snap.samba.smbd")
		_, err = os.Stat(cache)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor_parser was used to unload the profile
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--remove", "snap.samba.smbd"},
		})
	}
}

func (s *backendSuite) TestRemovingSnapWithHookRemovesAndUnloadsProfiles(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.HookYaml, 1)
		s.parserCmd.ForgetCalls()
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.foo.hook.configure")
		// file called "snap.foo.hook.configure" was removed
		_, err := os.Stat(profile)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor cache file was removed
		cache := filepath.Join(dirs.AppArmorCacheDir, "snap.foo.hook.configure")
		_, err = os.Stat(cache)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor_parser was used to unload the profile
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--remove", "snap.foo.hook.configure"},
		})
	}
}

func (s *backendSuite) TestUpdatingSnapMakesNeccesaryChanges(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 1)
		s.parserCmd.ForgetCalls()
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 2)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		// apparmor_parser was used to reload the profile because snap revision
		// is inside the generated policy.
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), "--quiet", profile},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreApps(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 1)
		s.parserCmd.ForgetCalls()
		// NOTE: the revision is kept the same to just test on the new application being added
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1WithNmbd, 1)
		smbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		nmbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.nmbd")
		// file called "snap.sambda.nmbd" was created
		_, err := os.Stat(nmbdProfile)
		c.Check(err, IsNil)
		// apparmor_parser was used to load the both profiles
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), "--quiet", nmbdProfile},
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), "--quiet", smbdProfile},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreHooks(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1WithNmbd, 1)
		s.parserCmd.ForgetCalls()
		// NOTE: the revision is kept the same to just test on the new application being added
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlWithHook, 1)
		smbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		nmbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.nmbd")
		hookProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.hook.configure")

		// Verify that profile "snap.samba.hook.configure" was created
		_, err := os.Stat(hookProfile)
		c.Check(err, IsNil)
		// apparmor_parser was used to load the both profiles
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), "--quiet", hookProfile},
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), "--quiet", nmbdProfile},
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), "--quiet", smbdProfile},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerApps(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1WithNmbd, 1)
		s.parserCmd.ForgetCalls()
		// NOTE: the revision is kept the same to just test on the application being removed
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 1)
		smbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		nmbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.nmbd")
		// file called "snap.sambda.nmbd" was removed
		_, err := os.Stat(nmbdProfile)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor_parser was used to remove the unused profile
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), "--quiet", smbdProfile},
			{"apparmor_parser", "--remove", "snap.samba.nmbd"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerHooks(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlWithHook, 1)
		s.parserCmd.ForgetCalls()
		// NOTE: the revision is kept the same to just test on the application being removed
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1WithNmbd, 1)
		smbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		nmbdProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.nmbd")
		hookProfile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.hook.configure")

		// Verify profile "snap.samba.hook.configure" was removed
		_, err := os.Stat(hookProfile)
		c.Check(os.IsNotExist(err), Equals, true)
		// apparmor_parser was used to remove the unused profile
		c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), "--quiet", nmbdProfile},
			{"apparmor_parser", "--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s/var/cache/apparmor", s.RootDir), "--quiet", smbdProfile},
			{"apparmor_parser", "--remove", "snap.samba.hook.configure"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRealDefaultTemplateIsNormallyUsed(c *C) {
	restore := release.MockAppArmorLevel(release.FullAppArmor)
	defer restore()

	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlV1, nil)
	// NOTE: we don't call apparmor.MockTemplate()
	err := s.Backend.Setup(snapInfo, interfaces.ConfinementOptions{}, s.Repo)
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
	opts    interfaces.ConfinementOptions
	snippet string
	content string
}

const commonPrefix = `
@{SNAP_NAME}="samba"
@{SNAP_REVISION}="1"
@{PROFILE_DBUS}="snap_2esamba_2esmbd"
@{INSTALL_DIR}="/snap"`

var combineSnippetsScenarios = []combineSnippetsScenario{{
	// By default apparmor is enforcing mode.
	opts:    interfaces.ConfinementOptions{},
	content: commonPrefix + "\nprofile \"snap.samba.smbd\" (attach_disconnected) {\n\n}\n",
}, {
	// Snippets are injected in the space between "{" and "}"
	opts:    interfaces.ConfinementOptions{},
	snippet: "snippet",
	content: commonPrefix + "\nprofile \"snap.samba.smbd\" (attach_disconnected) {\nsnippet\n}\n",
}, {
	// DevMode switches apparmor to non-enforcing (complain) mode.
	opts:    interfaces.ConfinementOptions{DevMode: true},
	snippet: "snippet",
	content: commonPrefix + "\nprofile \"snap.samba.smbd\" (attach_disconnected,complain) {\nsnippet\n}\n",
}, {
	// JailMode switches apparmor to enforcing mode even in the presence of DevMode.
	opts:    interfaces.ConfinementOptions{DevMode: true},
	snippet: "snippet",
	content: commonPrefix + "\nprofile \"snap.samba.smbd\" (attach_disconnected,complain) {\nsnippet\n}\n",
}, {
	// Classic confinement (without jailmode) uses apparmor in complain mode by default and ignores all snippets.
	opts:    interfaces.ConfinementOptions{Classic: true},
	snippet: "snippet",
	content: "\n#classic" + commonPrefix + "\nprofile \"snap.samba.smbd\" (attach_disconnected,complain) {\n\n}\n",
}, {
	// Classic confinement in JailMode uses enforcing apparmor.
	opts:    interfaces.ConfinementOptions{Classic: true, JailMode: true},
	snippet: "snippet",
	content: commonPrefix + `
profile "snap.samba.smbd" (attach_disconnected) {

  # Read-only access to the core snap.
  @{INSTALL_DIR}/core/** r,
  # Read only access to the core snap to load libc from.
  # This is related to LP: #1666897
  @{INSTALL_DIR}/core/*/{,usr/}lib/@{multiarch}/{,**/}lib*.so* m,

snippet
}
`,
}}

func (s *backendSuite) TestCombineSnippets(c *C) {
	restore := release.MockAppArmorLevel(release.FullAppArmor)
	defer restore()
	restore = osutil.MockMountInfo("") // mock away NFS detection
	defer restore()

	// NOTE: replace the real template with a shorter variant
	restoreTemplate := apparmor.MockTemplate("\n" +
		"###VAR###\n" +
		"###PROFILEATTACH### (attach_disconnected) {\n" +
		"###SNIPPETS###\n" +
		"}\n")
	defer restoreTemplate()
	restoreClassicTemplate := apparmor.MockClassicTemplate("\n" +
		"#classic\n" +
		"###VAR###\n" +
		"###PROFILEATTACH### (attach_disconnected) {\n" +
		"###SNIPPETS###\n" +
		"}\n")
	defer restoreClassicTemplate()
	for _, scenario := range combineSnippetsScenarios {
		s.Iface.AppArmorPermanentSlotCallback = func(spec *apparmor.Specification, slot *snap.SlotInfo) error {
			if scenario.snippet == "" {
				return nil
			}
			spec.AddSnippet(scenario.snippet)
			return nil
		}
		snapInfo := s.InstallSnap(c, scenario.opts, ifacetest.SambaYamlV1, 1)
		profile := filepath.Join(dirs.SnapAppArmorDir, "snap.samba.smbd")
		data, err := ioutil.ReadFile(profile)
		c.Assert(err, IsNil)
		c.Check(string(data), Equals, scenario.content)
		stat, err := os.Stat(profile)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}

var coreYaml string = `name: core
version: 1
`

func (s *backendSuite) TestSetupHostSnapConfineApparmorForReexecCleans(c *C) {
	restorer := release.MockOnClassic(true)
	defer restorer()
	restorer = release.MockForcedDevmode(false)
	defer restorer()

	canaryName := strings.Replace(filepath.Join(dirs.SnapMountDir, "/core/2718/usr/lib/snapd/snap-confine"), "/", ".", -1)[1:]
	canary := filepath.Join(dirs.SystemApparmorDir, canaryName)
	err := os.MkdirAll(filepath.Dir(canary), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(canary, nil, 0644)
	c.Assert(err, IsNil)

	// install the new core snap on classic triggers cleanup
	s.InstallSnap(c, interfaces.ConfinementOptions{}, coreYaml, 111)

	c.Check(osutil.FileExists(canary), Equals, false)
	c.Check(s.parserCmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "-R", canaryName},
	})
}

func (s *backendSuite) TestSetupHostSnapConfineApparmorForReexecWritesNew(c *C) {
	restorer := release.MockOnClassic(true)
	defer restorer()
	restorer = release.MockForcedDevmode(false)
	defer restorer()

	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()

	var mockAA = []byte(`# Author: Jamie Strandboge <jamie@canonical.com>
#include <tunables/global>

/usr/lib/snapd/snap-confine (attach_disconnected) {
    # We run privileged, so be fanatical about what we include and don't use
    # any abstractions
    /etc/ld.so.cache r,
}
`)

	err := os.MkdirAll(dirs.SystemApparmorDir, 0755)
	c.Assert(err, IsNil)

	// meh, the paths/filenames are all complicated :/
	coreRoot := filepath.Join(dirs.SnapMountDir, "/core/111")
	snapConfineApparmorInCore := filepath.Join(coreRoot, "/etc/apparmor.d/usr.lib.snapd.snap-confine.real")
	err = os.MkdirAll(filepath.Dir(snapConfineApparmorInCore), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(snapConfineApparmorInCore, mockAA, 0644)
	c.Assert(err, IsNil)

	// install the new core snap on classic triggers a new snap-confine
	// for this snap-confine on core
	s.InstallSnap(c, interfaces.ConfinementOptions{}, coreYaml, 111)

	newAA, err := filepath.Glob(filepath.Join(dirs.SystemApparmorDir, "*"))
	c.Assert(err, IsNil)

	c.Assert(newAA, HasLen, 1)
	c.Check(newAA[0], Matches, `.*/etc/apparmor.d/.*.snap.core.111.usr.lib.snapd.snap-confine`)

	content, err := ioutil.ReadFile(newAA[0])
	c.Assert(err, IsNil)
	// this is the key, rewriting "/usr/lib/snapd/snap-confine
	c.Check(string(content), testutil.Contains, "/snap/core/111/usr/lib/snapd/snap-confine (attach_disconnected) {")
	// no other changes other than that to the input
	c.Check(string(content), Equals, fmt.Sprintf(`# Author: Jamie Strandboge <jamie@canonical.com>
#include <tunables/global>

%s/core/111/usr/lib/snapd/snap-confine (attach_disconnected) {
    # We run privileged, so be fanatical about what we include and don't use
    # any abstractions
    /etc/ld.so.cache r,
}
`, dirs.SnapMountDir))

	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"apparmor_parser", "--replace", "--write-cache", newAA[0], "--cache-loc", dirs.SystemApparmorCacheDir},
	})

	// snap-confine directory was created
	_, err = os.Stat(dirs.SnapConfineAppArmorDir)
	c.Check(err, IsNil)

}

func (s *backendSuite) TestCoreOnCoreCleansApparmorCache(c *C) {
	restorer := release.MockOnClassic(false)
	defer restorer()

	err := os.MkdirAll(dirs.SystemApparmorCacheDir, 0755)
	c.Assert(err, IsNil)
	// the canary file in the cache will be removed
	canaryPath := filepath.Join(dirs.SystemApparmorCacheDir, "meep")
	err = ioutil.WriteFile(canaryPath, nil, 0644)
	c.Assert(err, IsNil)
	// but non-regular entries in the cache dir are kept
	dirsAreKept := filepath.Join(dirs.SystemApparmorCacheDir, "dir")
	err = os.MkdirAll(dirsAreKept, 0755)
	c.Assert(err, IsNil)
	symlinksAreKept := filepath.Join(dirs.SystemApparmorCacheDir, "symlink")
	err = os.Symlink("some-sylink-target", symlinksAreKept)
	c.Assert(err, IsNil)

	// install the new core snap on classic triggers a new snap-confine
	// for this snap-confine on core
	s.InstallSnap(c, interfaces.ConfinementOptions{}, coreYaml, 111)

	l, err := filepath.Glob(filepath.Join(dirs.SystemApparmorCacheDir, "*"))
	c.Assert(err, IsNil)
	// canary is gone, extra stuff is kept
	c.Check(l, DeepEquals, []string{dirsAreKept, symlinksAreKept})
}

func (s *backendSuite) TestIsHomeUsingNFS(c *C) {
	cases := []struct {
		mountinfo, fstab string
		nfs              bool
		errorPattern     string
	}{{
		// Errors from parsing mountinfo and fstab are propagated.
		mountinfo:    "bad syntax",
		errorPattern: "cannot parse .*/mountinfo.*, .*",
	}, {
		fstab:        "bad syntax",
		errorPattern: "cannot parse .*/fstab.*, .*",
	}, {
		// NFSv3 {tcp,udp} and NFSv4 currently mounted at /home/zyga/nfs are recognized.
		mountinfo: "1074 28 0:59 / /home/zyga/nfs rw,relatime shared:342 - nfs localhost:/srv/nfs rw,vers=3,rsize=1048576,wsize=1048576,namlen=255,hard,proto=tcp,timeo=600,retrans=2,sec=sys,mountaddr=127.0.0.1,mountvers=3,mountport=54125,mountproto=tcp,local_lock=none,addr=127.0.0.1",
		nfs:       true,
	}, {
		mountinfo: "1074 28 0:59 / /home/zyga/nfs rw,relatime shared:342 - nfs localhost:/srv/nfs rw,vers=3,rsize=32768,wsize=32768,namlen=255,hard,proto=udp,timeo=11,retrans=3,sec=sys,mountaddr=127.0.0.1,mountvers=3,mountport=47875,mountproto=udp,local_lock=none,addr=127.0.0.1",
		nfs:       true,
	}, {
		mountinfo: "680 27 0:59 / /home/zyga/nfs rw,relatime shared:478 - nfs4 localhost:/srv/nfs rw,vers=4.2,rsize=524288,wsize=524288,namlen=255,hard,proto=tcp,port=0,timeo=600,retrans=2,sec=sys,clientaddr=127.0.0.1,local_lock=none,addr=127.0.0.1",
		nfs:       true,
	}, {
		// NFSv3 {tcp,udp} and NFSv4 currently mounted at /home/zyga/nfs are ignored (not in $HOME).
		mountinfo: "1074 28 0:59 / /mnt/nfs rw,relatime shared:342 - nfs localhost:/srv/nfs rw,vers=3,rsize=1048576,wsize=1048576,namlen=255,hard,proto=tcp,timeo=600,retrans=2,sec=sys,mountaddr=127.0.0.1,mountvers=3,mountport=54125,mountproto=tcp,local_lock=none,addr=127.0.0.1",
	}, {
		mountinfo: "1074 28 0:59 / /mnt/nfs rw,relatime shared:342 - nfs localhost:/srv/nfs rw,vers=3,rsize=32768,wsize=32768,namlen=255,hard,proto=udp,timeo=11,retrans=3,sec=sys,mountaddr=127.0.0.1,mountvers=3,mountport=47875,mountproto=udp,local_lock=none,addr=127.0.0.1",
	}, {
		mountinfo: "680 27 0:59 / /mnt/nfs rw,relatime shared:478 - nfs4 localhost:/srv/nfs rw,vers=4.2,rsize=524288,wsize=524288,namlen=255,hard,proto=tcp,port=0,timeo=600,retrans=2,sec=sys,clientaddr=127.0.0.1,local_lock=none,addr=127.0.0.1",
	}, {
		// NFS that may be mounted at /home and /home/zyga/nfs is recognized.
		// Two spellings are possible, "nfs" and "nfs4" (they are equivalent
		// nowadays).
		fstab: "localhost:/srv/nfs /home nfs defaults 0 0",
		nfs:   true,
	}, {
		fstab: "localhost:/srv/nfs /home nfs4 defaults 0 0",
		nfs:   true,
	}, {
		fstab: "localhost:/srv/nfs /home/zyga/nfs nfs defaults 0 0",
		nfs:   true,
	}, {
		fstab: "localhost:/srv/nfs /home/zyga/nfs nfs4 defaults 0 0",
		nfs:   true,
	}, {
		// NFS that may be mounted at /mnt/nfs is ignored (not in $HOME).
		fstab: "localhost:/srv/nfs /mnt/nfs nfs defaults 0 0",
	}}
	for _, tc := range cases {
		restore := osutil.MockMountInfo(tc.mountinfo)
		defer restore()
		restore = osutil.MockEtcFstab(tc.fstab)
		defer restore()

		nfs, err := osutil.IsHomeUsingNFS()
		if tc.errorPattern != "" {
			c.Assert(err, ErrorMatches, tc.errorPattern, Commentf("test case %#v", tc))
		} else {
			c.Assert(err, IsNil)
		}
		c.Assert(nfs, Equals, tc.nfs)
	}
}

// snap-confine policy when NFS is not used.
func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyNoNFS(c *C) {
	// Make it appear as if NFS was not used.
	restore := osutil.MockMountInfo("")
	defer restore()
	restore = osutil.MockEtcFstab("")
	defer restore()

	// Intercept interaction with apparmor_parser
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()

	// Setup generated policy for snap-confine.
	err := (&apparmor.Backend{}).Initialize()
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), HasLen, 0)

	// Because NFS is not used there are no local policy files but the
	// directory was created.
	files, err := ioutil.ReadDir(dirs.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 0)

	// The policy was not reloaded.
	c.Assert(cmd.Calls(), HasLen, 0)
}

func MockEnableNFSWorkaroundCondition() (restore func()) {
	// Mock mountinfo and fstab so that snapd thinks that NFS workaround
	// is necessary. The details don't matter here. See TestIsHomeUsingNFS
	// for details about what triggers the workaround.
	restore1 := osutil.MockMountInfo("")
	restore2 := osutil.MockEtcFstab("localhost:/srv/nfs /home nfs4 defaults 0 0")
	return func() {
		restore1()
		restore2()
	}
}

func MockDisableNFSWorkaroundCondition() (restore func()) {
	// Mock mountinfo and fstab so that snapd thinks that NFS workaround is not
	// necessary. The details don't matter here. See TestIsHomeUsingNFS for
	// details about what triggers the workaround.
	restore1 := osutil.MockMountInfo("")
	restore2 := osutil.MockEtcFstab("")
	return func() {
		restore1()
		restore2()
	}
}

// Ensure that both names of the snap-confine apparmor profile are supported.

func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyWithNFS1(c *C) {
	s.testSetupSnapConfineGeneratedPolicyWithNFS(c, "usr.lib.snapd.snap-confine")
}

func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyWithNFS2(c *C) {
	s.testSetupSnapConfineGeneratedPolicyWithNFS(c, "usr.lib.snapd.snap-confine.real")
}

// snap-confine policy when NFS is used and snapd has not re-executed.
func (s *backendSuite) testSetupSnapConfineGeneratedPolicyWithNFS(c *C, profileFname string) {
	// Make it appear as if NFS workaround was needed.
	restore := MockEnableNFSWorkaroundCondition()
	defer restore()

	// Intercept interaction with apparmor_parser
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()

	// Intercept the /proc/self/exe symlink and point it to the distribution
	// executable (the path doesn't matter as long as it is not from the
	// mounted core snap). This indicates that snapd is not re-executing
	// and that we should reload snap-confine profile.
	fakeExe := filepath.Join(s.RootDir, "fake-proc-self-exe")
	err := os.Symlink("/usr/lib/snapd/snapd", fakeExe)
	c.Assert(err, IsNil)
	restore = apparmor.MockProcSelfExe(fakeExe)
	defer restore()

	profilePath := filepath.Join(dirs.SystemApparmorDir, profileFname)

	// Create the directory where system apparmor profiles are stored and write
	// the system apparmor profile of snap-confine.
	c.Assert(os.MkdirAll(dirs.SystemApparmorDir, 0755), IsNil)
	c.Assert(ioutil.WriteFile(profilePath, []byte(""), 0644), IsNil)

	// Setup generated policy for snap-confine.
	err = (&apparmor.Backend{}).Initialize()
	c.Assert(err, IsNil)

	// Because NFS is being used, we have the extra policy file.
	files, err := ioutil.ReadDir(dirs.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 1)
	c.Assert(files[0].Name(), Equals, "nfs-support")
	c.Assert(files[0].Mode(), Equals, os.FileMode(0644))
	c.Assert(files[0].IsDir(), Equals, false)

	// The policy allows network access.
	data, err := ioutil.ReadFile(filepath.Join(dirs.SnapConfineAppArmorDir, files[0].Name()))
	c.Assert(err, IsNil)
	c.Assert(string(data), testutil.Contains, "network inet,")
	c.Assert(string(data), testutil.Contains, "network inet6,")

	// The system apparmor profile of snap-confine was reloaded.
	c.Assert(cmd.Calls(), HasLen, 1)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{{
		"apparmor_parser", "--replace",
		"-O", "no-expr-simplify",
		"--write-cache",
		"--cache-loc", dirs.SystemApparmorCacheDir,
		profilePath,
	}})
}

// snap-confine policy when NFS is used and snapd has re-executed.
func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyWithNFSAndReExec(c *C) {
	// Make it appear as if NFS workaround was needed.
	restore := MockEnableNFSWorkaroundCondition()
	defer restore()

	// Intercept interaction with apparmor_parser
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()

	// Intercept the /proc/self/exe symlink and point it to the snapd from the
	// mounted core snap. This indicates that snapd has re-executed and
	// should not reload snap-confine policy.
	fakeExe := filepath.Join(s.RootDir, "fake-proc-self-exe")
	err := os.Symlink(filepath.Join(dirs.SnapMountDir, "/core/1234/usr/lib/snapd/snapd"), fakeExe)
	c.Assert(err, IsNil)
	restore = apparmor.MockProcSelfExe(fakeExe)
	defer restore()

	// Setup generated policy for snap-confine.
	err = (&apparmor.Backend{}).Initialize()
	c.Assert(err, IsNil)

	// Because NFS is being used, we have the extra policy file.
	files, err := ioutil.ReadDir(dirs.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 1)
	c.Assert(files[0].Name(), Equals, "nfs-support")
	c.Assert(files[0].Mode(), Equals, os.FileMode(0644))
	c.Assert(files[0].IsDir(), Equals, false)

	// The policy allows network access.
	data, err := ioutil.ReadFile(filepath.Join(dirs.SnapConfineAppArmorDir, files[0].Name()))
	c.Assert(err, IsNil)
	c.Assert(string(data), testutil.Contains, "network inet,")
	c.Assert(string(data), testutil.Contains, "network inet6,")

	// The distribution policy was not reloaded because snap-confine executes
	// from core snap. This is handled separately by per-profile Setup.
	c.Assert(cmd.Calls(), HasLen, 0)
}

// Test behavior when isHomeUsingNFS fails.
func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyError1(c *C) {
	// Make corrupt mountinfo.
	restore := osutil.MockMountInfo("corrupt")
	defer restore()

	// Intercept interaction with apparmor_parser
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()

	// Intercept the /proc/self/exe symlink and point it to the snapd from the
	// distribution.  This indicates that snapd has not re-executed and should
	// reload snap-confine policy.
	fakeExe := filepath.Join(s.RootDir, "fake-proc-self-exe")
	err := os.Symlink(filepath.Join(dirs.SnapMountDir, "/usr/lib/snapd/snapd"), fakeExe)
	c.Assert(err, IsNil)
	restore = apparmor.MockProcSelfExe(fakeExe)
	defer restore()

	// Setup generated policy for snap-confine.
	err = (&apparmor.Backend{}).Initialize()
	// NOTE: Errors in determining NFS are non-fatal to prevent snapd from
	// failing to operate. A warning message is logged but system operates as
	// if NFS was not active.
	c.Assert(err, IsNil)

	// While other stuff failed we created the policy directory and didn't
	// write any files to it.
	files, err := ioutil.ReadDir(dirs.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 0)

	// We didn't reload the policy.
	c.Assert(cmd.Calls(), HasLen, 0)
}

// Test behavior when os.Readlink "/proc/self/exe" fails.
func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyError2(c *C) {
	// Make it appear as if NFS workaround was needed.
	restore := MockEnableNFSWorkaroundCondition()
	defer restore()

	// Intercept interaction with apparmor_parser
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()

	// Intercept the /proc/self/exe symlink and make it point to something that
	// doesn't exist (break it).
	fakeExe := filepath.Join(s.RootDir, "corrupt-proc-self-exe")
	restore = apparmor.MockProcSelfExe(fakeExe)
	defer restore()

	// Setup generated policy for snap-confine.
	err := (&apparmor.Backend{}).Initialize()
	c.Assert(err, ErrorMatches, "cannot read .*corrupt-proc-self-exe: .*")

	// We didn't create the policy file.
	files, err := ioutil.ReadDir(dirs.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 0)

	// We didn't reload the policy though.
	c.Assert(cmd.Calls(), HasLen, 0)
}

// Test behavior when exec.Command "apparmor_parser" fails
func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyError3(c *C) {
	// Make it appear as if NFS workaround was needed.
	restore := MockEnableNFSWorkaroundCondition()
	defer restore()

	// Intercept interaction with apparmor_parser and make it fail.
	cmd := testutil.MockCommand(c, "apparmor_parser", "echo testing; exit 1")
	defer cmd.Restore()

	// Intercept the /proc/self/exe symlink.
	fakeExe := filepath.Join(s.RootDir, "fake-proc-self-exe")
	err := os.Symlink("/usr/lib/snapd/snapd", fakeExe)
	c.Assert(err, IsNil)
	restore = apparmor.MockProcSelfExe(fakeExe)
	defer restore()

	// Create the directory where system apparmor profiles are stored and Write
	// the system apparmor profile of snap-confine.
	c.Assert(os.MkdirAll(dirs.SystemApparmorDir, 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(dirs.SystemApparmorDir, "usr.lib.snapd.snap-confine"), []byte(""), 0644), IsNil)

	// Setup generated policy for snap-confine.
	err = (&apparmor.Backend{}).Initialize()
	c.Assert(err, ErrorMatches, "cannot reload snap-confine apparmor profile: testing")

	// While created the policy file initially we also removed it so that
	// no side-effects remain.
	files, err := ioutil.ReadDir(dirs.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 0)

	// We tried to reload the policy.
	c.Assert(cmd.Calls(), HasLen, 1)
}

// Test behavior when MkdirAll fails
func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyError4(c *C) {
	// Create a directory where we would expect to find the local policy.
	err := ioutil.WriteFile(dirs.SnapConfineAppArmorDir, []byte(""), 0644)
	c.Assert(err, IsNil)

	// Setup generated policy for snap-confine.
	err = (&apparmor.Backend{}).Initialize()
	c.Assert(err, ErrorMatches, "*.: not a directory")
}

// Test behavior when EnsureDirState fails
func (s *backendSuite) TestSetupSnapConfineGeneratedPolicyError5(c *C) {
	// This test cannot run as root as root bypassed DAC checks.
	u, err := user.Current()
	c.Assert(err, IsNil)
	if u.Uid == "0" {
		c.Skip("this test cannot run as root")
	}

	// Make it appear as if NFS workaround was not needed.
	restore := MockDisableNFSWorkaroundCondition()
	defer restore()

	// Intercept interaction with apparmor_parser and make it fail.
	cmd := testutil.MockCommand(c, "apparmor_parser", "")
	defer cmd.Restore()

	// Intercept the /proc/self/exe symlink.
	fakeExe := filepath.Join(s.RootDir, "fake-proc-self-exe")
	err = os.Symlink("/usr/lib/snapd/snapd", fakeExe)
	c.Assert(err, IsNil)
	restore = apparmor.MockProcSelfExe(fakeExe)
	defer restore()

	// Create the snap-confine directory and put a file. Because the file name
	// matches the glob generated-* snapd will attempt to remove it but because
	// the directory is not writable, that operation will fail.
	err = os.MkdirAll(dirs.SnapConfineAppArmorDir, 0755)
	c.Assert(err, IsNil)
	f := filepath.Join(dirs.SnapConfineAppArmorDir, "generated-test")
	err = ioutil.WriteFile(f, []byte("spurious content"), 0644)
	c.Assert(err, IsNil)
	err = os.Chmod(dirs.SnapConfineAppArmorDir, 0555)
	c.Assert(err, IsNil)

	// Make the directory writable for cleanup.
	defer os.Chmod(dirs.SnapConfineAppArmorDir, 0755)

	// Setup generated policy for snap-confine.
	err = (&apparmor.Backend{}).Initialize()
	c.Assert(err, ErrorMatches, `cannot synchronize snap-confine policy: remove .*/generated-test: permission denied`)

	// The policy directory was unchanged.
	files, err := ioutil.ReadDir(dirs.SnapConfineAppArmorDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 1)

	// We didn't try to reload the policy.
	c.Assert(cmd.Calls(), HasLen, 0)
}
