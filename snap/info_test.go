// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snap_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snap/squashfs"
)

type infoSuite struct {
	restore func()
}

var _ = Suite(&infoSuite{})

func (s *infoSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	hookType := snap.NewHookType(regexp.MustCompile(".*"))
	s.restore = snap.MockSupportedHookTypes([]*snap.HookType{hookType})
}

func (s *infoSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	s.restore()
}

func (s *infoSuite) TestSideInfoOverrides(c *C) {
	info := &snap.Info{
		SuggestedName:       "name",
		OriginalSummary:     "summary",
		OriginalDescription: "desc",
	}

	info.SideInfo = snap.SideInfo{
		RealName:          "newname",
		EditedSummary:     "fixed summary",
		EditedDescription: "fixed desc",
		Revision:          snap.R(1),
		SnapID:            "snapidsnapidsnapidsnapidsnapidsn",
	}

	c.Check(info.Name(), Equals, "newname")
	c.Check(info.Summary(), Equals, "fixed summary")
	c.Check(info.Description(), Equals, "fixed desc")
	c.Check(info.Revision, Equals, snap.R(1))
	c.Check(info.SnapID, Equals, "snapidsnapidsnapidsnapidsnapidsn")
}

func (s *infoSuite) TestAppInfoSecurityTag(c *C) {
	appInfo := &snap.AppInfo{Snap: &snap.Info{SuggestedName: "http"}, Name: "GET"}
	c.Check(appInfo.SecurityTag(), Equals, "snap.http.GET")
}

func (s *infoSuite) TestPlugSlotSecurityTags(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: name
apps:
    app1:
    app2:
hooks:
    hook1:
plugs:
    plug:
slots:
    slot:
`))
	c.Assert(err, IsNil)
	c.Assert(info.Plugs["plug"].SecurityTags(), DeepEquals, []string{
		"snap.name.app1", "snap.name.app2", "snap.name.hook.hook1"})
	c.Assert(info.Slots["slot"].SecurityTags(), DeepEquals, []string{
		"snap.name.app1", "snap.name.app2"})
}

func (s *infoSuite) TestAppInfoWrapperPath(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: foo
apps:
   foo:
   bar:
`))
	c.Assert(err, IsNil)

	c.Check(info.Apps["bar"].WrapperPath(), Equals, filepath.Join(dirs.SnapBinariesDir, "foo.bar"))
	c.Check(info.Apps["foo"].WrapperPath(), Equals, filepath.Join(dirs.SnapBinariesDir, "foo"))
}

func (s *infoSuite) TestAppInfoLauncherCommand(c *C) {
	dirs.SetRootDir("")

	info, err := snap.InfoFromSnapYaml([]byte(`name: foo
apps:
   foo:
     command: foo-bin
   bar:
     command: bar-bin -x
`))
	c.Assert(err, IsNil)
	info.Revision = snap.R(42)
	c.Check(info.Apps["bar"].LauncherCommand(), Equals, "/usr/bin/snap run foo.bar")
	c.Check(info.Apps["foo"].LauncherCommand(), Equals, "/usr/bin/snap run foo")
}

const sampleYaml = `
name: sample
version: 1
apps:
 app:
   command: foo
 app2:
   command: bar
 sample:
   command: foobar
`

const sampleContents = "SNAP"

func (s *infoSuite) TestReadInfo(c *C) {
	si := &snap.SideInfo{Revision: snap.R(42), EditedSummary: "esummary"}

	snapInfo1 := snaptest.MockSnap(c, sampleYaml, sampleContents, si)

	snapInfo2, err := snap.ReadInfo("sample", si)
	c.Assert(err, IsNil)

	c.Check(snapInfo2.Name(), Equals, "sample")
	c.Check(snapInfo2.Revision, Equals, snap.R(42))
	c.Check(snapInfo2.Summary(), Equals, "esummary")

	c.Check(snapInfo2.Apps["app"].Command, Equals, "foo")

	c.Check(snapInfo2, DeepEquals, snapInfo1)
}

// makeTestSnap here can also be used to produce broken snaps (differently from snaptest.MakeTestSnapWithFiles)!
func makeTestSnap(c *C, yaml string) string {
	tmp := c.MkDir()
	snapSource := filepath.Join(tmp, "snapsrc")

	err := os.MkdirAll(filepath.Join(snapSource, "meta"), 0755)
	c.Assert(err, IsNil)

	// our regular snap.yaml
	err = ioutil.WriteFile(filepath.Join(snapSource, "meta", "snap.yaml"), []byte(yaml), 0644)
	c.Assert(err, IsNil)

	dest := filepath.Join(tmp, "foo.snap")
	snap := squashfs.New(dest)
	err = snap.Build(snapSource)
	c.Assert(err, IsNil)

	return dest
}

// produce descrs for empty hooks suitable for snaptest.PopulateDir
func emptyHooks(hookNames ...string) (emptyHooks [][]string) {
	for _, hookName := range hookNames {
		emptyHooks = append(emptyHooks, []string{filepath.Join("meta", "hooks", hookName), ""})
	}
	return
}

func (s *infoSuite) TestReadInfoFromSnapFile(c *C) {
	yaml := `name: foo
version: 1.0
type: app
epoch: 1*
confinement: devmode`
	snapPath := snaptest.MakeTestSnapWithFiles(c, yaml, nil)

	snapf, err := snap.Open(snapPath)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, IsNil)
	c.Check(info.Name(), Equals, "foo")
	c.Check(info.Version, Equals, "1.0")
	c.Check(info.Type, Equals, snap.TypeApp)
	c.Check(info.Revision, Equals, snap.R(0))
	c.Check(info.Epoch, Equals, "1*")
	c.Check(info.Confinement, Equals, snap.DevModeConfinement)
}

func (s *infoSuite) TestReadInfoFromSnapFileMissingEpoch(c *C) {
	yaml := `name: foo
version: 1.0
type: app`
	snapPath := snaptest.MakeTestSnapWithFiles(c, yaml, nil)

	snapf, err := snap.Open(snapPath)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, IsNil)
	c.Check(info.Name(), Equals, "foo")
	c.Check(info.Version, Equals, "1.0")
	c.Check(info.Type, Equals, snap.TypeApp)
	c.Check(info.Revision, Equals, snap.R(0))
	c.Check(info.Epoch, Equals, "0") // Defaults to 0
}

func (s *infoSuite) TestReadInfoFromSnapFileWithSideInfo(c *C) {
	yaml := `name: foo
version: 1.0
type: app`
	snapPath := snaptest.MakeTestSnapWithFiles(c, yaml, nil)

	snapf, err := snap.Open(snapPath)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, &snap.SideInfo{
		RealName: "baz",
		Revision: snap.R(42),
	})
	c.Assert(err, IsNil)
	c.Check(info.Name(), Equals, "baz")
	c.Check(info.Version, Equals, "1.0")
	c.Check(info.Type, Equals, snap.TypeApp)
	c.Check(info.Revision, Equals, snap.R(42))
}

func (s *infoSuite) TestReadInfoFromSnapFileValidates(c *C) {
	yaml := `name: foo.bar
version: 1.0
type: app`
	snapPath := makeTestSnap(c, yaml)

	snapf, err := snap.Open(snapPath)
	c.Assert(err, IsNil)

	_, err = snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, ErrorMatches, "invalid snap name.*")
}

func (s *infoSuite) TestReadInfoFromSnapFileCatchesInvalidType(c *C) {
	yaml := `name: foo
version: 1.0
type: foo`
	snapPath := makeTestSnap(c, yaml)

	snapf, err := snap.Open(snapPath)
	c.Assert(err, IsNil)

	_, err = snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, ErrorMatches, ".*invalid snap type.*")
}

func (s *infoSuite) TestReadInfoFromSnapFileCatchesInvalidConfinement(c *C) {
	yaml := `name: foo
version: 1.0
confinement: foo`
	snapPath := makeTestSnap(c, yaml)

	snapf, err := snap.Open(snapPath)
	c.Assert(err, IsNil)

	_, err = snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, ErrorMatches, ".*invalid confinement type.*")
}

func (s *infoSuite) TestAppEnvSimple(c *C) {
	yaml := `name: foo
version: 1.0
type: app
environment:
 global-k: global-v
apps:
 foo:
  environment:
   app-k: app-v
`
	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	env := info.Apps["foo"].Env()
	sort.Strings(env)
	c.Check(env, DeepEquals, []string{
		"app-k=app-v",
		"global-k=global-v",
	})
}

func (s *infoSuite) TestAppEnvOverrideGlobal(c *C) {
	yaml := `name: foo
version: 1.0
type: app
environment:
 global-k: global-v
 global-and-local: global-v
apps:
 foo:
  environment:
   app-k: app-v
   global-and-local: local-v
`
	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	env := info.Apps["foo"].Env()
	sort.Strings(env)
	c.Check(env, DeepEquals, []string{
		"app-k=app-v",
		"global-and-local=local-v",
		"global-k=global-v",
	})
}

func (s *infoSuite) TestSplitSnapApp(c *C) {
	for _, t := range []struct {
		in  string
		out []string
	}{
		// normal cases
		{"foo.bar", []string{"foo", "bar"}},
		{"foo.bar.baz", []string{"foo", "bar.baz"}},
		// special case, snapName == appName
		{"foo", []string{"foo", "foo"}},
	} {
		snap, app := snap.SplitSnapApp(t.in)
		c.Check([]string{snap, app}, DeepEquals, t.out)
	}
}

func ExampleSpltiSnapApp() {
	fmt.Println(snap.SplitSnapApp("hello-world.env"))
	// Output: hello-world env
}

func ExampleSpltiSnapAppShort() {
	fmt.Println(snap.SplitSnapApp("hello-world"))
	// Output: hello-world hello-world
}

func (s *infoSuite) TestReadInfoFromSnapFileCatchesInvalidHook(c *C) {
	yaml := `name: foo
version: 1.0
hooks:
  123abc:`
	snapPath := makeTestSnap(c, yaml)

	snapf, err := snap.Open(snapPath)
	c.Assert(err, IsNil)

	_, err = snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, ErrorMatches, ".*invalid hook name.*")
}

func (s *infoSuite) TestReadInfoFromSnapFileCatchesInvalidImplicitHook(c *C) {
	yaml := `name: foo
version: 1.0`
	snapPath := snaptest.MakeTestSnapWithFiles(c, yaml, emptyHooks("123abc"))

	snapf, err := snap.Open(snapPath)
	c.Assert(err, IsNil)

	_, err = snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, ErrorMatches, ".*invalid hook name.*")
}

func (s *infoSuite) checkInstalledSnapAndSnapFile(c *C, yaml string, contents string, hooks []string, checker func(c *C, info *snap.Info)) {
	// First check installed snap
	sideInfo := &snap.SideInfo{Revision: snap.R(42)}
	info0 := snaptest.MockSnap(c, yaml, contents, sideInfo)
	snaptest.PopulateDir(info0.MountDir(), emptyHooks(hooks...))
	info, err := snap.ReadInfo(info0.Name(), sideInfo)
	c.Check(err, IsNil)
	checker(c, info)

	// Now check snap file
	snapPath := snaptest.MakeTestSnapWithFiles(c, yaml, emptyHooks(hooks...))
	snapf, err := snap.Open(snapPath)
	c.Assert(err, IsNil)
	info, err = snap.ReadInfoFromSnapFile(snapf, nil)
	c.Check(err, IsNil)
	checker(c, info)
}

func (s *infoSuite) TestReadInfoNoHooks(c *C) {
	yaml := `name: foo
version: 1.0`
	s.checkInstalledSnapAndSnapFile(c, yaml, "SNAP", nil, func(c *C, info *snap.Info) {
		// Verify that no hooks were loaded for this snap
		c.Check(info.Hooks, HasLen, 0)
	})
}

func (s *infoSuite) TestReadInfoSingleImplicitHook(c *C) {
	yaml := `name: foo
version: 1.0`
	s.checkInstalledSnapAndSnapFile(c, yaml, "SNAP", []string{"test-hook"}, func(c *C, info *snap.Info) {
		// Verify that the `test-hook` hook has now been loaded, and that it has
		// no associated plugs.
		c.Check(info.Hooks, HasLen, 1)
		verifyImplicitHook(c, info, "test-hook")
	})
}

func (s *infoSuite) TestReadInfoMultipleImplicitHooks(c *C) {
	yaml := `name: foo
version: 1.0`
	s.checkInstalledSnapAndSnapFile(c, yaml, "SNAP", []string{"foo", "bar"}, func(c *C, info *snap.Info) {
		// Verify that both hooks have now been loaded, and that neither have any
		// associated plugs.
		c.Check(info.Hooks, HasLen, 2)
		verifyImplicitHook(c, info, "foo")
		verifyImplicitHook(c, info, "bar")
	})
}

func (s *infoSuite) TestReadInfoInvalidImplicitHook(c *C) {
	hookType := snap.NewHookType(regexp.MustCompile("foo"))
	s.restore = snap.MockSupportedHookTypes([]*snap.HookType{hookType})

	yaml := `name: foo
version: 1.0`
	s.checkInstalledSnapAndSnapFile(c, yaml, "SNAP", []string{"foo", "bar"}, func(c *C, info *snap.Info) {
		// Verify that only foo has been loaded, not bar
		c.Check(info.Hooks, HasLen, 1)
		verifyImplicitHook(c, info, "foo")
	})
}

func (s *infoSuite) TestReadInfoImplicitAndExplicitHooks(c *C) {
	yaml := `name: foo
version: 1.0
hooks:
  explicit:
    plugs: [test-plug]`
	s.checkInstalledSnapAndSnapFile(c, yaml, "SNAP", []string{"explicit", "implicit"}, func(c *C, info *snap.Info) {
		// Verify that the `implicit` hook has now been loaded, and that it has
		// no associated plugs. Also verify that the `explicit` hook is still
		// valid.
		c.Check(info.Hooks, HasLen, 2)
		verifyImplicitHook(c, info, "implicit")
		verifyExplicitHook(c, info, "explicit", []string{"test-plug"})
	})
}

func verifyImplicitHook(c *C, info *snap.Info, hookName string) {
	hook := info.Hooks[hookName]
	c.Assert(hook, NotNil, Commentf("Expected hooks to contain %q", hookName))
	c.Check(hook.Name, Equals, hookName)
	c.Check(hook.Plugs, IsNil)
}

func verifyExplicitHook(c *C, info *snap.Info, hookName string, plugNames []string) {
	hook := info.Hooks[hookName]
	c.Assert(hook, NotNil, Commentf("Expected hooks to contain %q", hookName))
	c.Check(hook.Name, Equals, hookName)
	c.Check(hook.Plugs, HasLen, len(plugNames))

	for _, plugName := range plugNames {
		// Verify that the HookInfo and PlugInfo point to each other
		plug := hook.Plugs[plugName]
		c.Assert(plug, NotNil, Commentf("Expected hook plugs to contain %q", plugName))
		c.Check(plug.Name, Equals, plugName)
		c.Check(plug.Hooks, HasLen, 1)
		hook = plug.Hooks[hookName]
		c.Assert(hook, NotNil, Commentf("Expected plug to be associated with hook %q", hookName))
		c.Check(hook.Name, Equals, hookName)

		// Verify also that the hook plug made it into info.Plugs
		c.Check(info.Plugs[plugName], DeepEquals, plug)
	}
}

func (s *infoSuite) TestDirAndFileMethods(c *C) {
	dirs.SetRootDir("")
	info := &snap.Info{SuggestedName: "name", SideInfo: snap.SideInfo{Revision: snap.R(1)}}
	c.Check(info.MountDir(), Equals, fmt.Sprintf("%s/name/1", dirs.SnapMountDir))
	c.Check(info.MountFile(), Equals, "/var/lib/snapd/snaps/name_1.snap")
	c.Check(info.HooksDir(), Equals, fmt.Sprintf("%s/name/1/meta/hooks", dirs.SnapMountDir))
	c.Check(info.DataDir(), Equals, "/var/snap/name/1")
	c.Check(info.UserDataDir("/home/bob"), Equals, "/home/bob/snap/name/1")
	c.Check(info.UserCommonDataDir("/home/bob"), Equals, "/home/bob/snap/name/common")
	c.Check(info.CommonDataDir(), Equals, "/var/snap/name/common")
	c.Check(info.UserXdgRuntimeDir(12345), Equals, "/run/user/12345/snap.name")
	// XXX: Those are actually a globs, not directories
	c.Check(info.DataHomeDir(), Equals, "/home/*/snap/name/1")
	c.Check(info.CommonDataHomeDir(), Equals, "/home/*/snap/name/common")
	c.Check(info.XdgRuntimeDirs(), Equals, "/run/user/*/snap.name")
}

func makeFakeDesktopFile(c *C, name, content string) string {
	df := filepath.Join(dirs.SnapDesktopFilesDir, name)
	err := os.MkdirAll(filepath.Dir(df), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(df, []byte(content), 0644)
	c.Assert(err, IsNil)
	return df
}

func (s *infoSuite) TestAppDesktopFile(c *C) {
	snaptest.MockSnap(c, sampleYaml, sampleContents, &snap.SideInfo{})
	snapInfo, err := snap.ReadInfo("sample", &snap.SideInfo{})
	c.Assert(err, IsNil)

	c.Check(snapInfo.Name(), Equals, "sample")
	c.Check(snapInfo.Apps["app"].DesktopFile(), Matches, `.*/var/lib/snapd/desktop/applications/sample_app.desktop`)
	c.Check(snapInfo.Apps["sample"].DesktopFile(), Matches, `.*/var/lib/snapd/desktop/applications/sample_sample.desktop`)
}

func (s *infoSuite) TestRenamePlug(c *C) {
	snapInfo := snaptest.MockInvalidInfo(c, `name: core
plugs:
  old:
    interface: iface
slots:
  old:
    interface: iface
apps:
  app:
hooks:
  configure:
`, nil)
	c.Assert(snapInfo.Plugs["old"], Not(IsNil))
	c.Assert(snapInfo.Plugs["old"].Name, Equals, "old")
	c.Assert(snapInfo.Slots["old"], Not(IsNil))
	c.Assert(snapInfo.Slots["old"].Name, Equals, "old")
	c.Assert(snapInfo.Apps["app"].Plugs["old"], DeepEquals, snapInfo.Plugs["old"])
	c.Assert(snapInfo.Apps["app"].Slots["old"], DeepEquals, snapInfo.Slots["old"])
	c.Assert(snapInfo.Hooks["configure"].Plugs["old"], DeepEquals, snapInfo.Plugs["old"])

	// Rename the plug now.
	snapInfo.RenamePlug("old", "new")

	// Check that there's no trace of the old plug name.
	c.Assert(snapInfo.Plugs["old"], IsNil)
	c.Assert(snapInfo.Plugs["new"], Not(IsNil))
	c.Assert(snapInfo.Plugs["new"].Name, Equals, "new")
	c.Assert(snapInfo.Apps["app"].Plugs["old"], IsNil)
	c.Assert(snapInfo.Apps["app"].Plugs["new"], DeepEquals, snapInfo.Plugs["new"])
	c.Assert(snapInfo.Hooks["configure"].Plugs["old"], IsNil)
	c.Assert(snapInfo.Hooks["configure"].Plugs["new"], DeepEquals, snapInfo.Plugs["new"])

	// Check that slots with the old name are unaffected.
	c.Assert(snapInfo.Slots["old"], Not(IsNil))
	c.Assert(snapInfo.Slots["old"].Name, Equals, "old")
	c.Assert(snapInfo.Apps["app"].Slots["old"], DeepEquals, snapInfo.Slots["old"])

	// Check that the rename made the snap valid now
	c.Assert(snap.Validate(snapInfo), IsNil)
}

const coreSnapYaml = `name: core
type: os
plugs:
  network-bind:
  core-support:
`

// reading snap via ReadInfoFromSnapFile renames clashing core plugs
func (s *infoSuite) TestReadInfoFromSnapFileRenamesCorePlus(c *C) {
	snapPath := snaptest.MakeTestSnapWithFiles(c, coreSnapYaml, nil)

	snapf, err := snap.Open(snapPath)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, IsNil)
	c.Check(info.Plugs["network-bind"], IsNil)
	c.Check(info.Plugs["core-support"], IsNil)
	c.Check(info.Plugs["network-bind-plug"], NotNil)
	c.Check(info.Plugs["core-support-plug"], NotNil)
}

// reading snap via ReadInfo renames clashing core plugs
func (s *infoSuite) TestReadInfoRenamesCorePlugs(c *C) {
	si := &snap.SideInfo{Revision: snap.R(42), RealName: "core"}
	snaptest.MockSnap(c, coreSnapYaml, sampleContents, si)
	info, err := snap.ReadInfo("core", si)
	c.Assert(err, IsNil)
	c.Check(info.Plugs["network-bind"], IsNil)
	c.Check(info.Plugs["core-support"], IsNil)
	c.Check(info.Plugs["network-bind-plug"], NotNil)
	c.Check(info.Plugs["core-support-plug"], NotNil)
}

// reading snap via InfoFromSnapYaml renames clashing core plugs
func (s *infoSuite) TestInfoFromSnapYamlRenamesCorePlugs(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(coreSnapYaml))
	c.Assert(err, IsNil)
	c.Check(info.Plugs["network-bind"], IsNil)
	c.Check(info.Plugs["core-support"], IsNil)
	c.Check(info.Plugs["network-bind-plug"], NotNil)
	c.Check(info.Plugs["core-support-plug"], NotNil)
}
