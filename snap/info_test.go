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
	"github.com/snapcore/snapd/testutil"
)

type infoSuite struct {
	testutil.BaseTest
}

type infoSimpleSuite struct{}

var _ = Suite(&infoSuite{})
var _ = Suite(&infoSimpleSuite{})

func (s *infoSimpleSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *infoSimpleSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *infoSimpleSuite) TestReadInfoPanicsIfSanitizeUnset(c *C) {
	si := &snap.SideInfo{Revision: snap.R(1)}
	snaptest.MockSnap(c, sampleYaml, sampleContents, si)
	c.Assert(func() { snap.ReadInfo("sample", si) }, Panics, `SanitizePlugsSlots function not set`)
}

func (s *infoSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	hookType := snap.NewHookType(regexp.MustCompile(".*"))
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	s.BaseTest.AddCleanup(snap.MockSupportedHookTypes([]*snap.HookType{hookType}))
}

func (s *infoSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")
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
		"snap.name.app1", "snap.name.app2", "snap.name.hook.hook1"})
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

func (s *infoSuite) TestAppInfoCompleterPath(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: foo
apps:
   foo:
   bar:
`))
	c.Assert(err, IsNil)

	c.Check(info.Apps["bar"].CompleterPath(), Equals, filepath.Join(dirs.CompletersDir, "foo.bar"))
	c.Check(info.Apps["foo"].CompleterPath(), Equals, filepath.Join(dirs.CompletersDir, "foo"))
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
	c.Check(info.Apps["bar"].LauncherStopCommand(), Equals, "/usr/bin/snap run --command=stop foo.bar")
	c.Check(info.Apps["bar"].LauncherReloadCommand(), Equals, "/usr/bin/snap run --command=reload foo.bar")
	c.Check(info.Apps["bar"].LauncherPostStopCommand(), Equals, "/usr/bin/snap run --command=post-stop foo.bar")
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

func (s *infoSuite) TestReadInfoNotFound(c *C) {
	si := &snap.SideInfo{Revision: snap.R(42), EditedSummary: "esummary"}
	info, err := snap.ReadInfo("sample", si)
	c.Check(info, IsNil)
	c.Check(err, ErrorMatches, `cannot find installed snap "sample" at revision 42`)
}

func (s *infoSuite) TestReadInfoUnreadable(c *C) {
	si := &snap.SideInfo{Revision: snap.R(42), EditedSummary: "esummary"}
	c.Assert(os.MkdirAll(filepath.Join(snap.MinimalPlaceInfo("sample", si.Revision).MountDir(), "meta", "snap.yaml"), 0755), IsNil)

	info, err := snap.ReadInfo("sample", si)
	c.Check(info, IsNil)
	// TODO: maybe improve this error message
	c.Check(err, ErrorMatches, ".* is a directory")
}

func (s *infoSuite) TestReadInfoUnparsable(c *C) {
	si := &snap.SideInfo{Revision: snap.R(42), EditedSummary: "esummary"}
	p := filepath.Join(snap.MinimalPlaceInfo("sample", si.Revision).MountDir(), "meta", "snap.yaml")
	c.Assert(os.MkdirAll(filepath.Dir(p), 0755), IsNil)
	c.Assert(ioutil.WriteFile(p, []byte(`- :`), 0644), IsNil)

	info, err := snap.ReadInfo("sample", si)
	c.Check(info, IsNil)
	// TODO: maybe improve this error message
	c.Check(err, ErrorMatches, ".* failed to parse.*")
}

func (s *infoSuite) TestReadInfoUnfindable(c *C) {
	si := &snap.SideInfo{Revision: snap.R(42), EditedSummary: "esummary"}
	p := filepath.Join(snap.MinimalPlaceInfo("sample", si.Revision).MountDir(), "meta", "snap.yaml")
	c.Assert(os.MkdirAll(filepath.Dir(p), 0755), IsNil)
	c.Assert(ioutil.WriteFile(p, []byte(``), 0644), IsNil)

	info, err := snap.ReadInfo("sample", si)
	c.Check(info, IsNil)
	// TODO: maybe improve this error message
	c.Check(err, ErrorMatches, ".* no such file or directory")
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
	c.Check(info.Epoch.String(), Equals, "1*")
	c.Check(info.Confinement, Equals, snap.DevModeConfinement)
	c.Check(info.NeedsDevMode(), Equals, true)
	c.Check(info.NeedsClassic(), Equals, false)
}

func (s *infoSuite) TestReadInfoFromClassicSnapFile(c *C) {
	yaml := `name: foo
version: 1.0
type: app
confinement: classic`
	snapPath := snaptest.MakeTestSnapWithFiles(c, yaml, nil)

	snapf, err := snap.Open(snapPath)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, IsNil)
	c.Check(info.Name(), Equals, "foo")
	c.Check(info.Version, Equals, "1.0")
	c.Check(info.Type, Equals, snap.TypeApp)
	c.Check(info.Revision, Equals, snap.R(0))
	c.Check(info.Confinement, Equals, snap.ClassicConfinement)
	c.Check(info.NeedsDevMode(), Equals, false)
	c.Check(info.NeedsClassic(), Equals, true)
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
	c.Check(info.Epoch.String(), Equals, "0") // Defaults to 0
	c.Check(info.Confinement, Equals, snap.StrictConfinement)
	c.Check(info.NeedsDevMode(), Equals, false)
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

func (s *infoSuite) TestJoinSnapApp(c *C) {
	for _, t := range []struct {
		in  []string
		out string
	}{
		// normal cases
		{[]string{"foo", "bar"}, "foo.bar"},
		{[]string{"foo", "bar-baz"}, "foo.bar-baz"},
		// special case, snapName == appName
		{[]string{"foo", "foo"}, "foo"},
	} {
		snapApp := snap.JoinSnapApp(t.in[0], t.in[1])
		c.Check(snapApp, Equals, t.out)
	}
}

func ExampleSplitSnapApp() {
	fmt.Println(snap.SplitSnapApp("hello-world.env"))
	// Output: hello-world env
}

func ExampleSplitSnapAppShort() {
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
	s.BaseTest.AddCleanup(snap.MockSupportedHookTypes([]*snap.HookType{hookType}))

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
    plugs: [test-plug]
    slots: [test-slot]`
	s.checkInstalledSnapAndSnapFile(c, yaml, "SNAP", []string{"explicit", "implicit"}, func(c *C, info *snap.Info) {
		// Verify that the `implicit` hook has now been loaded, and that it has
		// no associated plugs. Also verify that the `explicit` hook is still
		// valid.
		c.Check(info.Hooks, HasLen, 2)
		verifyImplicitHook(c, info, "implicit")
		verifyExplicitHook(c, info, "explicit", []string{"test-plug"}, []string{"test-slot"})
	})
}

func verifyImplicitHook(c *C, info *snap.Info, hookName string) {
	hook := info.Hooks[hookName]
	c.Assert(hook, NotNil, Commentf("Expected hooks to contain %q", hookName))
	c.Check(hook.Name, Equals, hookName)
	c.Check(hook.Plugs, IsNil)
}

func verifyExplicitHook(c *C, info *snap.Info, hookName string, plugNames []string, slotNames []string) {
	hook := info.Hooks[hookName]
	c.Assert(hook, NotNil, Commentf("Expected hooks to contain %q", hookName))
	c.Check(hook.Name, Equals, hookName)
	c.Check(hook.Plugs, HasLen, len(plugNames))
	c.Check(hook.Slots, HasLen, len(slotNames))

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

	for _, slotName := range slotNames {
		// Verify that the HookInfo and SlotInfo point to each other
		slot := hook.Slots[slotName]
		c.Assert(slot, NotNil, Commentf("Expected hook slots to contain %q", slotName))
		c.Check(slot.Name, Equals, slotName)
		c.Check(slot.Hooks, HasLen, 1)
		hook = slot.Hooks[hookName]
		c.Assert(hook, NotNil, Commentf("Expected slot to be associated with hook %q", hookName))
		c.Check(hook.Name, Equals, hookName)

		// Verify also that the hook plug made it into info.Slots
		c.Check(info.Slots[slotName], DeepEquals, slot)
	}

}

func (s *infoSuite) TestMinimalInfoDirAndFileMethods(c *C) {
	dirs.SetRootDir("")
	info := snap.MinimalPlaceInfo("name", snap.R("1"))
	s.testDirAndFileMethods(c, info)
}

func (s *infoSuite) TestDirAndFileMethods(c *C) {
	dirs.SetRootDir("")
	info := &snap.Info{SuggestedName: "name", SideInfo: snap.SideInfo{Revision: snap.R(1)}}
	s.testDirAndFileMethods(c, info)
}

func (s *infoSuite) testDirAndFileMethods(c *C, info snap.PlaceInfo) {
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

func (s *infoSuite) TestInfoServices(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: pans
apps:
  svc1:
    daemon: potato
  svc2:
    daemon: no
  app1:
  app2:
`))
	c.Assert(err, IsNil)
	svcNames := []string{}
	svcs := info.Services()
	for i := range svcs {
		svcNames = append(svcNames, svcs[i].ServiceName())
	}
	sort.Strings(svcNames)
	c.Check(svcNames, DeepEquals, []string{
		"snap.pans.svc1.service",
		"snap.pans.svc2.service",
	})
}

func (s *infoSuite) TestAppInfoIsService(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: pans
apps:
  svc1:
    daemon: potato
  svc2:
    daemon: no
  app1:
  app2:
`))
	c.Assert(err, IsNil)

	svc := info.Apps["svc1"]
	c.Check(svc.IsService(), Equals, true)
	c.Check(svc.ServiceName(), Equals, "snap.pans.svc1.service")
	c.Check(svc.ServiceFile(), Equals, dirs.GlobalRootDir+"/etc/systemd/system/snap.pans.svc1.service")

	c.Check(info.Apps["svc2"].IsService(), Equals, true)
	c.Check(info.Apps["app1"].IsService(), Equals, false)
	c.Check(info.Apps["app1"].IsService(), Equals, false)
}

func (s *infoSuite) TestSocketFile(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: pans
apps:
  app1:
    daemon: true
    sockets:
      sock1:
        listen-stream: /tmp/sock1.socket
`))

	c.Assert(err, IsNil)

	app := info.Apps["app1"]
	socket := app.Sockets["sock1"]
	c.Check(socket.File(), Equals, dirs.GlobalRootDir+"/etc/systemd/system/snap.pans.app1.sock1.socket")
}

func (s *infoSuite) TestLayoutParsing(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: layout-demo
layout:
  /usr:
    bind: $SNAP/usr
  /mytmp:
    type: tmpfs
    user: nobody
    group: nobody
    mode: 1777
  /mylink:
    symlink: /link/target
`))
	c.Assert(err, IsNil)

	layout := info.Layout
	c.Assert(layout, NotNil)
	c.Check(layout["/usr"], DeepEquals, &snap.Layout{
		Snap:  info,
		Path:  "/usr",
		User:  "root",
		Group: "root",
		Mode:  0755,
		Bind:  "$SNAP/usr",
	})
	c.Check(layout["/mytmp"], DeepEquals, &snap.Layout{
		Snap:  info,
		Path:  "/mytmp",
		Type:  "tmpfs",
		User:  "nobody",
		Group: "nobody",
		Mode:  01777,
	})
	c.Check(layout["/mylink"], DeepEquals, &snap.Layout{
		Snap:    info,
		Path:    "/mylink",
		User:    "root",
		Group:   "root",
		Mode:    0755,
		Symlink: "/link/target",
	})
}

func (s *infoSuite) TestPlugInfoString(c *C) {
	plug := &snap.PlugInfo{Snap: &snap.Info{SuggestedName: "snap"}, Name: "plug"}
	c.Assert(plug.String(), Equals, "snap:plug")
}

func (s *infoSuite) TestSlotInfoString(c *C) {
	slot := &snap.SlotInfo{Snap: &snap.Info{SuggestedName: "snap"}, Name: "slot"}
	c.Assert(slot.String(), Equals, "snap:slot")
}

func (s *infoSuite) TestPlugInfoAttr(c *C) {
	plug := &snap.PlugInfo{Snap: &snap.Info{SuggestedName: "snap"}, Name: "plug", Attrs: map[string]interface{}{"key": "value"}}
	v, err := plug.Attr("key")
	c.Assert(err, IsNil)
	c.Check(v, Equals, "value")

	_, err = plug.Attr("unknown")
	c.Check(err, ErrorMatches, `attribute "unknown" not found`)
}

func (s *infoSuite) TestSlotInfoAttr(c *C) {
	slot := &snap.SlotInfo{Snap: &snap.Info{SuggestedName: "snap"}, Name: "plug", Attrs: map[string]interface{}{"key": "value"}}
	v, err := slot.Attr("key")
	c.Assert(err, IsNil)
	c.Check(v, Equals, "value")

	_, err = slot.Attr("unknown")
	c.Check(err, ErrorMatches, `attribute "unknown" not found`)
}
