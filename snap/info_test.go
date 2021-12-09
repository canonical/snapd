// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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
	"strings"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
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
	snaptest.MockSnap(c, sampleYaml, si)
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

	c.Check(info.InstanceName(), Equals, "newname")
	c.Check(info.Summary(), Equals, "fixed summary")
	c.Check(info.Description(), Equals, "fixed desc")
	c.Check(info.Revision, Equals, snap.R(1))
	c.Check(info.SnapID, Equals, "snapidsnapidsnapidsnapidsnapidsn")
	c.Check(info.ID(), Equals, "snapidsnapidsnapidsnapidsnapidsn")
}

func (s *infoSuite) TestContactFromEdited(c *C) {
	info := &snap.Info{
		OriginalLinks: nil,
	}

	info.SideInfo = snap.SideInfo{
		EditedContact: "mailto:econtact",
	}

	c.Check(info.Contact(), Equals, "mailto:econtact")
}

func (s *infoSuite) TestNoContact(c *C) {
	info := &snap.Info{}

	c.Check(info.Contact(), Equals, "")
}

func (s *infoSuite) TestContactFromLinks(c *C) {
	info := &snap.Info{
		OriginalLinks: map[string][]string{
			"contact": {"ocontact1", "ocontact2"},
		},
	}

	c.Check(info.Contact(), Equals, "mailto:ocontact1")
}

func (s *infoSuite) TestContactFromLinksMailtoAlready(c *C) {
	info := &snap.Info{
		OriginalLinks: map[string][]string{
			"contact": {"mailto:ocontact1", "ocontact2"},
		},
	}

	c.Check(info.Contact(), Equals, "mailto:ocontact1")
}

func (s *infoSuite) TestContactFromLinksNotEmail(c *C) {
	info := &snap.Info{
		OriginalLinks: map[string][]string{
			"contact": {"https://ocontact1", "ocontact2"},
		},
	}

	c.Check(info.Contact(), Equals, "https://ocontact1")
}

func (s *infoSuite) TestLinks(c *C) {
	info := &snap.Info{
		OriginalLinks: map[string][]string{
			"contact": {"ocontact"},
			"website": {"owebsite"},
		},
	}

	info.SideInfo = snap.SideInfo{
		EditedLinks: map[string][]string{
			"contact": {"econtact"},
			"website": {"ewebsite"},
		},
	}

	c.Check(info.Links(), DeepEquals, map[string][]string{
		"contact": {"econtact"},
		"website": {"ewebsite"},
	})

	info.EditedLinks = nil
	c.Check(info.Links(), DeepEquals, map[string][]string{
		"contact": {"ocontact"},
		"website": {"owebsite"},
	})
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
   baz:
     command: bar-bin -x
     timer: 10:00-12:00,,mon,12:00~14:00
`))
	c.Assert(err, IsNil)
	info.Revision = snap.R(42)
	c.Check(info.Apps["bar"].LauncherCommand(), Equals, "/usr/bin/snap run foo.bar")
	c.Check(info.Apps["bar"].LauncherStopCommand(), Equals, "/usr/bin/snap run --command=stop foo.bar")
	c.Check(info.Apps["bar"].LauncherReloadCommand(), Equals, "/usr/bin/snap run --command=reload foo.bar")
	c.Check(info.Apps["bar"].LauncherPostStopCommand(), Equals, "/usr/bin/snap run --command=post-stop foo.bar")
	c.Check(info.Apps["foo"].LauncherCommand(), Equals, "/usr/bin/snap run foo")
	c.Check(info.Apps["baz"].LauncherCommand(), Equals, `/usr/bin/snap run --timer="10:00-12:00,,mon,12:00~14:00" foo.baz`)

	// snap with instance key
	info.InstanceKey = "instance"
	c.Check(info.Apps["bar"].LauncherCommand(), Equals, "/usr/bin/snap run foo_instance.bar")
	c.Check(info.Apps["bar"].LauncherStopCommand(), Equals, "/usr/bin/snap run --command=stop foo_instance.bar")
	c.Check(info.Apps["bar"].LauncherReloadCommand(), Equals, "/usr/bin/snap run --command=reload foo_instance.bar")
	c.Check(info.Apps["bar"].LauncherPostStopCommand(), Equals, "/usr/bin/snap run --command=post-stop foo_instance.bar")
	c.Check(info.Apps["foo"].LauncherCommand(), Equals, "/usr/bin/snap run foo_instance")
	c.Check(info.Apps["baz"].LauncherCommand(), Equals, `/usr/bin/snap run --timer="10:00-12:00,,mon,12:00~14:00" foo_instance.baz`)
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
   command-chain: [chain]
hooks:
 configure:
  command-chain: [hookchain]
`

func (s *infoSuite) TestReadInfo(c *C) {
	si := &snap.SideInfo{Revision: snap.R(42), EditedSummary: "esummary"}

	snapInfo1 := snaptest.MockSnap(c, sampleYaml, si)

	snapInfo2, err := snap.ReadInfo("sample", si)
	c.Assert(err, IsNil)

	c.Check(snapInfo2.InstanceName(), Equals, "sample")
	c.Check(snapInfo2.Revision, Equals, snap.R(42))
	c.Check(snapInfo2.Summary(), Equals, "esummary")

	c.Check(snapInfo2.Apps["app"].Command, Equals, "foo")
	c.Check(snapInfo2.Apps["sample"].CommandChain, DeepEquals, []string{"chain"})
	c.Check(snapInfo2.Hooks["configure"].CommandChain, DeepEquals, []string{"hookchain"})

	c.Check(snapInfo2, DeepEquals, snapInfo1)
}

func (s *infoSuite) TestReadInfoWithInstance(c *C) {
	si := &snap.SideInfo{Revision: snap.R(42), EditedSummary: "instance summary"}

	snapInfo1 := snaptest.MockSnapInstance(c, "sample_instance", sampleYaml, si)

	snapInfo2, err := snap.ReadInfo("sample_instance", si)
	c.Assert(err, IsNil)

	c.Check(snapInfo2.InstanceName(), Equals, "sample_instance")
	c.Check(snapInfo2.SnapName(), Equals, "sample")
	c.Check(snapInfo2.Revision, Equals, snap.R(42))
	c.Check(snapInfo2.Summary(), Equals, "instance summary")

	c.Check(snapInfo2.Apps["app"].Command, Equals, "foo")
	c.Check(snapInfo2.Apps["sample"].CommandChain, DeepEquals, []string{"chain"})
	c.Check(snapInfo2.Hooks["configure"].CommandChain, DeepEquals, []string{"hookchain"})

	c.Check(snapInfo2, DeepEquals, snapInfo1)
}

func (s *infoSuite) TestReadCurrentInfo(c *C) {
	si := &snap.SideInfo{Revision: snap.R(42)}

	snapInfo1 := snaptest.MockSnapCurrent(c, sampleYaml, si)

	snapInfo2, err := snap.ReadCurrentInfo("sample")
	c.Assert(err, IsNil)

	c.Check(snapInfo2.InstanceName(), Equals, "sample")
	c.Check(snapInfo2.Revision, Equals, snap.R(42))
	c.Check(snapInfo2, DeepEquals, snapInfo1)

	snapInfo3, err := snap.ReadCurrentInfo("not-sample")
	c.Check(snapInfo3, IsNil)
	c.Assert(err, ErrorMatches, `cannot find current revision for snap not-sample:.*`)
}

func (s *infoSuite) TestReadCurrentInfoWithInstance(c *C) {
	si := &snap.SideInfo{Revision: snap.R(42)}

	snapInfo1 := snaptest.MockSnapInstanceCurrent(c, "sample_instance", sampleYaml, si)

	snapInfo2, err := snap.ReadCurrentInfo("sample_instance")
	c.Assert(err, IsNil)

	c.Check(snapInfo2.InstanceName(), Equals, "sample_instance")
	c.Check(snapInfo2.SnapName(), Equals, "sample")
	c.Check(snapInfo2.Revision, Equals, snap.R(42))
	c.Check(snapInfo2, DeepEquals, snapInfo1)

	snapInfo3, err := snap.ReadCurrentInfo("sample_other")
	c.Check(snapInfo3, IsNil)
	c.Assert(err, ErrorMatches, `cannot find current revision for snap sample_other:.*`)
}

func (s *infoSuite) TestInstallDate(c *C) {
	si := &snap.SideInfo{Revision: snap.R(1)}
	info := snaptest.MockSnap(c, sampleYaml, si)
	// not current -> Zero
	c.Check(info.InstallDate().IsZero(), Equals, true)
	c.Check(snap.InstallDate(info.InstanceName()).IsZero(), Equals, true)

	mountdir := info.MountDir()
	dir, rev := filepath.Split(mountdir)
	c.Assert(os.MkdirAll(dir, 0755), IsNil)
	cur := filepath.Join(dir, "current")
	c.Assert(os.Symlink(rev, cur), IsNil)
	st, err := os.Lstat(cur)
	c.Assert(err, IsNil)
	instTime := st.ModTime()
	// sanity
	c.Check(instTime.IsZero(), Equals, false)

	c.Check(info.InstallDate().Equal(instTime), Equals, true)
	c.Check(snap.InstallDate(info.InstanceName()).Equal(instTime), Equals, true)
}

func (s *infoSuite) TestReadInfoNotFound(c *C) {
	si := &snap.SideInfo{Revision: snap.R(42), EditedSummary: "esummary"}
	info, err := snap.ReadInfo("sample", si)
	c.Check(info, IsNil)
	c.Check(err, ErrorMatches, `cannot find installed snap "sample" at revision 42: missing file .*sample/42/meta/snap.yaml`)
	bse, ok := err.(snap.BrokenSnapError)
	c.Assert(ok, Equals, true)
	c.Check(bse.Broken(), Equals, bse.Error())
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
	c.Check(err, ErrorMatches, `cannot use installed snap "sample" at revision 42: cannot parse snap.yaml: yaml: .*`)
	bse, ok := err.(snap.BrokenSnapError)
	c.Assert(ok, Equals, true)
	c.Check(bse.Broken(), Equals, bse.Error())
}

func (s *infoSuite) TestReadInfoUnfindable(c *C) {
	si := &snap.SideInfo{Revision: snap.R(42), EditedSummary: "esummary"}
	p := filepath.Join(snap.MinimalPlaceInfo("sample", si.Revision).MountDir(), "meta", "snap.yaml")
	c.Assert(os.MkdirAll(filepath.Dir(p), 0755), IsNil)
	c.Assert(ioutil.WriteFile(p, []byte(``), 0644), IsNil)

	info, err := snap.ReadInfo("sample", si)
	c.Check(err, ErrorMatches, `cannot find installed snap "sample" at revision 42: missing file .*var/lib/snapd/snaps/sample_42.snap`)
	c.Check(info, IsNil)
}

func (s *infoSuite) TestReadInfoDanglingSymlink(c *C) {
	si := &snap.SideInfo{Revision: snap.R(42), EditedSummary: "esummary"}
	mpi := snap.MinimalPlaceInfo("sample", si.Revision)
	p := filepath.Join(mpi.MountDir(), "meta", "snap.yaml")
	c.Assert(os.MkdirAll(filepath.Dir(p), 0755), IsNil)
	c.Assert(ioutil.WriteFile(p, []byte(`name: test`), 0644), IsNil)
	c.Assert(os.MkdirAll(filepath.Dir(mpi.MountFile()), 0755), IsNil)
	c.Assert(os.Symlink("/dangling", mpi.MountFile()), IsNil)

	info, err := snap.ReadInfo("sample", si)
	c.Check(err, IsNil)
	c.Check(info.SnapName(), Equals, "test")
	c.Check(info.Revision, Equals, snap.R(42))
	c.Check(info.Summary(), Equals, "esummary")
	c.Check(info.Size, Equals, int64(0))
}

// makeTestSnap here can also be used to produce broken snaps (differently from snaptest.MakeTestSnapWithFiles)!
func makeTestSnap(c *C, snapYaml string) string {
	var m struct {
		Type string `yaml:"type"`
	}
	yaml.Unmarshal([]byte(snapYaml), &m) // yes, ignore the error

	tmp := c.MkDir()
	snapSource := filepath.Join(tmp, "snapsrc")

	err := os.MkdirAll(filepath.Join(snapSource, "meta"), 0755)
	c.Assert(err, IsNil)

	// our regular snap.yaml
	err = ioutil.WriteFile(filepath.Join(snapSource, "meta", "snap.yaml"), []byte(snapYaml), 0644)
	c.Assert(err, IsNil)

	dest := filepath.Join(tmp, "foo.snap")
	snap := squashfs.New(dest)
	err = snap.Build(snapSource, &squashfs.BuildOpts{SnapType: m.Type})
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

	snapf, err := snapfile.Open(snapPath)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "foo")
	c.Check(info.Version, Equals, "1.0")
	c.Check(info.Type(), Equals, snap.TypeApp)
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

	snapf, err := snapfile.Open(snapPath)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "foo")
	c.Check(info.Version, Equals, "1.0")
	c.Check(info.Type(), Equals, snap.TypeApp)
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

	snapf, err := snapfile.Open(snapPath)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "foo")
	c.Check(info.Version, Equals, "1.0")
	c.Check(info.Type(), Equals, snap.TypeApp)
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

	snapf, err := snapfile.Open(snapPath)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, &snap.SideInfo{
		RealName: "baz",
		Revision: snap.R(42),
	})
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "baz")
	c.Check(info.Version, Equals, "1.0")
	c.Check(info.Type(), Equals, snap.TypeApp)
	c.Check(info.Revision, Equals, snap.R(42))
}

func (s *infoSuite) TestReadInfoFromSnapFileValidates(c *C) {
	yaml := `name: foo.bar
version: 1.0
type: app`
	snapPath := makeTestSnap(c, yaml)

	snapf, err := snapfile.Open(snapPath)
	c.Assert(err, IsNil)

	_, err = snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, ErrorMatches, `invalid snap name.*`)
}

func (s *infoSuite) TestReadInfoFromSnapFileCatchesInvalidType(c *C) {
	yaml := `name: foo
version: 1.0
type: foo`
	snapPath := makeTestSnap(c, yaml)

	snapf, err := snapfile.Open(snapPath)
	c.Assert(err, IsNil)

	_, err = snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, ErrorMatches, ".*invalid snap type.*")
}

func (s *infoSuite) TestReadInfoFromSnapFileCatchesInvalidConfinement(c *C) {
	yaml := `name: foo
version: 1.0
confinement: foo`
	snapPath := makeTestSnap(c, yaml)

	snapf, err := snapfile.Open(snapPath)
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

	c.Check(info.Apps["foo"].EnvChain(), DeepEquals, []osutil.ExpandableEnv{
		osutil.NewExpandableEnv("global-k", "global-v"),
		osutil.NewExpandableEnv("app-k", "app-v"),
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

	c.Check(info.Apps["foo"].EnvChain(), DeepEquals, []osutil.ExpandableEnv{
		osutil.NewExpandableEnv("global-k", "global-v", "global-and-local", "global-v"),
		osutil.NewExpandableEnv("app-k", "app-v", "global-and-local", "local-v"),
	})
}

func (s *infoSuite) TestHookEnvSimple(c *C) {
	yaml := `name: foo
version: 1.0
type: app
environment:
 global-k: global-v
hooks:
 foo:
  environment:
   app-k: app-v
`
	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	c.Check(info.Hooks["foo"].EnvChain(), DeepEquals, []osutil.ExpandableEnv{
		osutil.NewExpandableEnv("global-k", "global-v"),
		osutil.NewExpandableEnv("app-k", "app-v"),
	})
}

func (s *infoSuite) TestHookEnvOverrideGlobal(c *C) {
	yaml := `name: foo
version: 1.0
type: app
environment:
 global-k: global-v
 global-and-local: global-v
hooks:
 foo:
  environment:
   app-k: app-v
   global-and-local: local-v
`
	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	c.Assert(err, IsNil)

	c.Check(info.Hooks["foo"].EnvChain(), DeepEquals, []osutil.ExpandableEnv{
		osutil.NewExpandableEnv("global-k", "global-v", "global-and-local", "global-v"),
		osutil.NewExpandableEnv("app-k", "app-v", "global-and-local", "local-v"),
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
		// snap instance names
		{"foo_instance.bar", []string{"foo_instance", "bar"}},
		{"foo_instance.bar.baz", []string{"foo_instance", "bar.baz"}},
		{"foo_instance", []string{"foo_instance", "foo"}},
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
		// snap instance names
		{[]string{"foo_instance", "bar"}, "foo_instance.bar"},
		{[]string{"foo_instance", "bar-baz"}, "foo_instance.bar-baz"},
		{[]string{"foo_instance", "foo"}, "foo_instance"},
	} {
		snapApp := snap.JoinSnapApp(t.in[0], t.in[1])
		c.Check(snapApp, Equals, t.out)
	}
}

func ExampleSplitSnapApp() {
	fmt.Println(snap.SplitSnapApp("hello-world.env"))
	// Output: hello-world env
}

func ExampleSplitSnapApp_short() {
	fmt.Println(snap.SplitSnapApp("hello-world"))
	// Output: hello-world hello-world
}

func (s *infoSuite) TestReadInfoFromSnapFileCatchesInvalidHook(c *C) {
	yaml := `name: foo
version: 1.0
hooks:
  123abc:`
	snapPath := makeTestSnap(c, yaml)

	snapf, err := snapfile.Open(snapPath)
	c.Assert(err, IsNil)

	_, err = snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, ErrorMatches, ".*invalid hook name.*")
}

func (s *infoSuite) TestReadInfoFromSnapFileCatchesInvalidImplicitHook(c *C) {
	yaml := `name: foo
version: 1.0`
	snapPath := snaptest.MakeTestSnapWithFiles(c, yaml, emptyHooks("123abc"))

	snapf, err := snapfile.Open(snapPath)
	c.Assert(err, IsNil)

	_, err = snap.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, ErrorMatches, ".*invalid hook name.*")
}

func (s *infoSuite) checkInstalledSnapAndSnapFile(c *C, instanceName, yaml string, contents string, hooks []string, checker func(c *C, info *snap.Info)) {
	// First check installed snap
	sideInfo := &snap.SideInfo{Revision: snap.R(42)}
	info0 := snaptest.MockSnapInstance(c, instanceName, yaml, sideInfo)
	snaptest.PopulateDir(info0.MountDir(), emptyHooks(hooks...))
	info, err := snap.ReadInfo(info0.InstanceName(), sideInfo)
	c.Check(err, IsNil)
	checker(c, info)

	// Now check snap file
	snapPath := snaptest.MakeTestSnapWithFiles(c, yaml, emptyHooks(hooks...))
	snapf, err := snapfile.Open(snapPath)
	c.Assert(err, IsNil)
	info, err = snap.ReadInfoFromSnapFile(snapf, nil)
	c.Check(err, IsNil)
	checker(c, info)
}

func (s *infoSuite) TestReadInfoNoHooks(c *C) {
	yaml := `name: foo
version: 1.0`
	s.checkInstalledSnapAndSnapFile(c, "foo", yaml, "SNAP", nil, func(c *C, info *snap.Info) {
		// Verify that no hooks were loaded for this snap
		c.Check(info.Hooks, HasLen, 0)
	})
}

func (s *infoSuite) TestReadInfoSingleImplicitHook(c *C) {
	yaml := `name: foo
version: 1.0`
	s.checkInstalledSnapAndSnapFile(c, "foo", yaml, "SNAP", []string{"test-hook"}, func(c *C, info *snap.Info) {
		// Verify that the `test-hook` hook has now been loaded, and that it has
		// no associated plugs.
		c.Check(info.Hooks, HasLen, 1)
		verifyImplicitHook(c, info, "test-hook", nil)
	})
}

func (s *infoSuite) TestReadInfoMultipleImplicitHooks(c *C) {
	yaml := `name: foo
version: 1.0`
	s.checkInstalledSnapAndSnapFile(c, "foo", yaml, "SNAP", []string{"foo", "bar"}, func(c *C, info *snap.Info) {
		// Verify that both hooks have now been loaded, and that neither have any
		// associated plugs.
		c.Check(info.Hooks, HasLen, 2)
		verifyImplicitHook(c, info, "foo", nil)
		verifyImplicitHook(c, info, "bar", nil)
	})
}

func (s *infoSuite) TestReadInfoInvalidImplicitHook(c *C) {
	hookType := snap.NewHookType(regexp.MustCompile("foo"))
	s.BaseTest.AddCleanup(snap.MockSupportedHookTypes([]*snap.HookType{hookType}))

	yaml := `name: foo
version: 1.0`
	s.checkInstalledSnapAndSnapFile(c, "foo", yaml, "SNAP", []string{"foo", "bar"}, func(c *C, info *snap.Info) {
		// Verify that only foo has been loaded, not bar
		c.Check(info.Hooks, HasLen, 1)
		verifyImplicitHook(c, info, "foo", nil)
	})
}

func (s *infoSuite) TestReadInfoImplicitAndExplicitHooks(c *C) {
	yaml := `name: foo
version: 1.0
hooks:
  explicit:
    plugs: [test-plug]
    slots: [test-slot]`
	s.checkInstalledSnapAndSnapFile(c, "foo", yaml, "SNAP", []string{"explicit", "implicit"}, func(c *C, info *snap.Info) {
		// Verify that the `implicit` hook has now been loaded, and that it has
		// no associated plugs. Also verify that the `explicit` hook is still
		// valid.
		c.Check(info.Hooks, HasLen, 2)
		verifyImplicitHook(c, info, "implicit", nil)
		verifyExplicitHook(c, info, "explicit", []string{"test-plug"}, []string{"test-slot"})
	})
}

func (s *infoSuite) TestReadInfoExplicitHooks(c *C) {
	yaml := `name: foo
version: 1.0
plugs:
  test-plug:
slots:
  test-slot:
hooks:
  explicit:
`
	s.checkInstalledSnapAndSnapFile(c, "foo", yaml, "SNAP", []string{"explicit"}, func(c *C, info *snap.Info) {
		c.Check(info.Hooks, HasLen, 1)
		verifyExplicitHook(c, info, "explicit", []string{"test-plug"}, []string{"test-slot"})
	})
}

func (s *infoSuite) TestReadInfoImplicitHookPlugWhenImplicitlyBoundToApp(c *C) {
	yaml := `name: foo
version: 1.0
plugs:
  test-plug:
apps:
  app:
`
	s.checkInstalledSnapAndSnapFile(c, "foo", yaml, "SNAP", []string{"implicit"}, func(c *C, info *snap.Info) {
		c.Check(info.Hooks, HasLen, 1)
		verifyImplicitHook(c, info, "implicit", []string{"test-plug"})
	})
}

func (s *infoSuite) TestReadInfoImplicitHookPlugWhenExplicitlyBoundToApp(c *C) {
	yaml := `name: foo
version: 1.0
plugs:
  test-plug:
apps:
  app:
    plugs: [test-plug]
`
	s.checkInstalledSnapAndSnapFile(c, "foo", yaml, "SNAP", []string{"implicit"}, func(c *C, info *snap.Info) {
		c.Check(info.Hooks, HasLen, 1)
		verifyImplicitHook(c, info, "implicit", nil)
	})
}

func (s *infoSuite) TestParallelInstanceReadInfoImplicitAndExplicitHooks(c *C) {
	yaml := `name: foo
version: 1.0
hooks:
  explicit:
    plugs: [test-plug]
    slots: [test-slot]`
	s.checkInstalledSnapAndSnapFile(c, "foo_instance", yaml, "SNAP", []string{"explicit", "implicit"}, func(c *C, info *snap.Info) {
		c.Check(info.Hooks, HasLen, 2)
		verifyImplicitHook(c, info, "implicit", nil)
		verifyExplicitHook(c, info, "explicit", []string{"test-plug"}, []string{"test-slot"})
	})
}

func (s *infoSuite) TestReadInfoImplicitHookWithTopLevelPlugSlots(c *C) {
	yaml1 := `name: snap-1
version: 1.0
plugs:
  test-plug:
slots:
  test-slot:
hooks:
  explicit:
    plugs: [test-plug,other-plug]
    slots: [test-slot,other-slot]
`
	s.checkInstalledSnapAndSnapFile(c, "snap-1", yaml1, "SNAP", []string{"implicit"}, func(c *C, info *snap.Info) {
		c.Check(info.Hooks, HasLen, 2)
		implicitHook := info.Hooks["implicit"]
		c.Assert(implicitHook, NotNil)
		c.Assert(implicitHook.Explicit, Equals, false)
		c.Assert(implicitHook.Plugs, HasLen, 0)
		c.Assert(implicitHook.Slots, HasLen, 0)

		c.Check(info.Plugs, HasLen, 2)
		c.Check(info.Slots, HasLen, 2)

		plug := info.Plugs["test-plug"]
		c.Assert(plug, NotNil)
		// implicit hook has not gained test-plug because it was already
		// associated with an app or a hook (here with the hook called
		// "explicit"). This is consistent with the hook called "implicit"
		// having been defined in the YAML but devoid of any interface
		// assignments.
		c.Assert(implicitHook.Plugs["test-plug"], IsNil)

		slot := info.Slots["test-slot"]
		c.Assert(slot, NotNil)
		c.Assert(implicitHook.Slots["test-slot"], IsNil)

		explicitHook := info.Hooks["explicit"]
		c.Assert(explicitHook, NotNil)
		c.Assert(explicitHook.Explicit, Equals, true)
		c.Assert(explicitHook.Plugs, HasLen, 2)
		c.Assert(explicitHook.Slots, HasLen, 2)

		plug = info.Plugs["test-plug"]
		c.Assert(plug, NotNil)
		c.Assert(explicitHook.Plugs["test-plug"], DeepEquals, plug)

		slot = info.Slots["test-slot"]
		c.Assert(slot, NotNil)
		c.Assert(explicitHook.Slots["test-slot"], DeepEquals, slot)
	})

	yaml2 := `name: snap-2
version: 1.0
plugs:
  test-plug:
slots:
  test-slot:
`
	s.checkInstalledSnapAndSnapFile(c, "snap-2", yaml2, "SNAP", []string{"implicit"}, func(c *C, info *snap.Info) {
		c.Check(info.Hooks, HasLen, 1)
		implicitHook := info.Hooks["implicit"]
		c.Assert(implicitHook, NotNil)
		c.Assert(implicitHook.Explicit, Equals, false)
		c.Assert(implicitHook.Plugs, HasLen, 1)
		c.Assert(implicitHook.Slots, HasLen, 1)

		c.Check(info.Plugs, HasLen, 1)
		c.Check(info.Slots, HasLen, 1)

		plug := info.Plugs["test-plug"]
		c.Assert(plug, NotNil)
		c.Assert(implicitHook.Plugs["test-plug"], DeepEquals, plug)

		slot := info.Slots["test-slot"]
		c.Assert(slot, NotNil)
		c.Assert(implicitHook.Slots["test-slot"], DeepEquals, slot)
	})

	yaml3 := `name: snap-3
version: 1.0
plugs:
  test-plug:
slots:
  test-slot:
`
	s.checkInstalledSnapAndSnapFile(c, "snap-3", yaml3, "SNAP", []string{"implicit-1", "implicit-2"}, func(c *C, info *snap.Info) {
		c.Check(info.Hooks, HasLen, 2)
		implicit1Hook := info.Hooks["implicit-1"]
		c.Assert(implicit1Hook, NotNil)
		c.Assert(implicit1Hook.Explicit, Equals, false)
		c.Assert(implicit1Hook.Plugs, HasLen, 1)
		c.Assert(implicit1Hook.Slots, HasLen, 1)

		implicit2Hook := info.Hooks["implicit-2"]
		c.Assert(implicit2Hook, NotNil)
		c.Assert(implicit2Hook.Explicit, Equals, false)
		c.Assert(implicit2Hook.Plugs, HasLen, 1)
		c.Assert(implicit2Hook.Slots, HasLen, 1)

		c.Check(info.Plugs, HasLen, 1)
		c.Check(info.Slots, HasLen, 1)

		plug := info.Plugs["test-plug"]
		c.Assert(plug, NotNil)
		c.Assert(implicit1Hook.Plugs["test-plug"], DeepEquals, plug)
		c.Assert(implicit2Hook.Plugs["test-plug"], DeepEquals, plug)

		slot := info.Slots["test-slot"]
		c.Assert(slot, NotNil)
		c.Assert(implicit1Hook.Slots["test-slot"], DeepEquals, slot)
		c.Assert(implicit2Hook.Slots["test-slot"], DeepEquals, slot)
	})

}

func verifyImplicitHook(c *C, info *snap.Info, hookName string, plugNames []string) {
	hook := info.Hooks[hookName]
	c.Assert(hook, NotNil, Commentf("Expected hooks to contain %q", hookName))
	c.Check(hook.Name, Equals, hookName)

	if len(plugNames) == 0 {
		c.Check(hook.Plugs, IsNil)
	}

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

func (s *infoSuite) TestPlaceInfoRevision(c *C) {
	info := snap.MinimalPlaceInfo("name", snap.R("1"))
	c.Check(info.SnapRevision(), Equals, snap.R("1"))
}

func (s *infoSuite) TestMinimalInfoDirAndFileMethods(c *C) {
	dirs.SetRootDir("")
	info := snap.MinimalPlaceInfo("name", snap.R("1"))
	s.testDirAndFileMethods(c, info)
}

func (s *infoSuite) TestDirAndFileMethods(c *C) {
	dirs.SetRootDir("")
	info := &snap.Info{SuggestedName: "name"}
	info.SideInfo = snap.SideInfo{Revision: snap.R(1)}
	s.testDirAndFileMethods(c, info)
}

func (s *infoSuite) testDirAndFileMethods(c *C, info snap.PlaceInfo) {
	c.Check(info.MountDir(), Equals, fmt.Sprintf("%s/name/1", dirs.SnapMountDir))
	c.Check(info.MountFile(), Equals, "/var/lib/snapd/snaps/name_1.snap")
	c.Check(info.HooksDir(), Equals, fmt.Sprintf("%s/name/1/meta/hooks", dirs.SnapMountDir))
	c.Check(info.DataDir(), Equals, "/var/snap/name/1")
	c.Check(info.UserDataDir("/home/bob", nil), Equals, "/home/bob/snap/name/1")
	c.Check(info.UserCommonDataDir("/home/bob", nil), Equals, "/home/bob/snap/name/common")
	c.Check(info.CommonDataDir(), Equals, "/var/snap/name/common")
	c.Check(info.UserXdgRuntimeDir(12345), Equals, "/run/user/12345/snap.name")
	// XXX: Those are actually a globs, not directories
	c.Check(info.DataHomeDir(nil), Equals, "/home/*/snap/name/1")
	c.Check(info.CommonDataHomeDir(nil), Equals, "/home/*/snap/name/common")
	c.Check(info.XdgRuntimeDirs(), Equals, "/run/user/*/snap.name")
}

func (s *infoSuite) TestMinimalInfoDirAndFileMethodsParallelInstall(c *C) {
	dirs.SetRootDir("")
	info := snap.MinimalPlaceInfo("name_instance", snap.R("1"))
	s.testInstanceDirAndFileMethods(c, info)
}

func (s *infoSuite) TestDirAndFileMethodsParallelInstall(c *C) {
	dirs.SetRootDir("")
	info := &snap.Info{SuggestedName: "name", InstanceKey: "instance"}
	info.SideInfo = snap.SideInfo{Revision: snap.R(1)}
	s.testInstanceDirAndFileMethods(c, info)
}

func (s *infoSuite) testInstanceDirAndFileMethods(c *C, info snap.PlaceInfo) {
	c.Check(info.MountDir(), Equals, fmt.Sprintf("%s/name_instance/1", dirs.SnapMountDir))
	c.Check(info.MountFile(), Equals, "/var/lib/snapd/snaps/name_instance_1.snap")
	c.Check(info.HooksDir(), Equals, fmt.Sprintf("%s/name_instance/1/meta/hooks", dirs.SnapMountDir))
	c.Check(info.DataDir(), Equals, "/var/snap/name_instance/1")
	c.Check(info.UserDataDir("/home/bob", nil), Equals, "/home/bob/snap/name_instance/1")
	c.Check(info.UserCommonDataDir("/home/bob", nil), Equals, "/home/bob/snap/name_instance/common")
	c.Check(info.CommonDataDir(), Equals, "/var/snap/name_instance/common")
	c.Check(info.UserXdgRuntimeDir(12345), Equals, "/run/user/12345/snap.name_instance")
	// XXX: Those are actually a globs, not directories
	c.Check(info.DataHomeDir(nil), Equals, "/home/*/snap/name_instance/1")
	c.Check(info.CommonDataHomeDir(nil), Equals, "/home/*/snap/name_instance/common")
	c.Check(info.XdgRuntimeDirs(), Equals, "/run/user/*/snap.name_instance")
}

func BenchmarkTestParsePlaceInfoFromSnapFileName(b *testing.B) {
	for n := 0; n < b.N; n++ {
		for _, sn := range []string{
			"core_21.snap",
			"kernel_41.snap",
			"some-long-kernel-name-kernel_82.snap",
			"what-is-this-core_111.snap",
		} {
			snap.ParsePlaceInfoFromSnapFileName(sn)
		}
	}
}

func (s *infoSuite) TestParsePlaceInfoFromSnapFileName(c *C) {
	tt := []struct {
		sn        string
		name      string
		rev       string
		expectErr string
	}{
		{sn: "", expectErr: "empty snap file name"},
		{sn: "name", expectErr: `snap file name "name" has invalid format \(missing '_'\)`},
		{sn: "name_", expectErr: `cannot parse revision in snap file name "name_": invalid snap revision: ""`},
		{sn: "name__", expectErr: "too many '_' in snap file name"},
		{sn: "_name.snap", expectErr: `snap file name \"_name.snap\" has invalid format \(no snap name before '_'\)`},
		{sn: "name_key.snap", expectErr: `cannot parse revision in snap file name "name_key.snap": invalid snap revision: "key"`},
		{sn: "name.snap", expectErr: `snap file name "name.snap" has invalid format \(missing '_'\)`},
		{sn: "name_12.snap", name: "name", rev: "12"},
		{sn: "name_key_12.snap", expectErr: "too many '_' in snap file name"},
	}
	for _, t := range tt {
		p, err := snap.ParsePlaceInfoFromSnapFileName(t.sn)
		if t.expectErr != "" {
			c.Check(err, ErrorMatches, t.expectErr)
		} else {
			c.Check(p.SnapName(), Equals, t.name)
			c.Check(p.SnapRevision(), Equals, snap.R(t.rev))
		}
	}
}

func (s *infoSuite) TestAppDesktopFile(c *C) {
	snaptest.MockSnap(c, sampleYaml, &snap.SideInfo{})
	snapInfo, err := snap.ReadInfo("sample", &snap.SideInfo{})
	c.Assert(err, IsNil)

	c.Check(snapInfo.InstanceName(), Equals, "sample")
	c.Check(snapInfo.Apps["app"].DesktopFile(), Matches, `.*/var/lib/snapd/desktop/applications/sample_app.desktop`)
	c.Check(snapInfo.Apps["sample"].DesktopFile(), Matches, `.*/var/lib/snapd/desktop/applications/sample_sample.desktop`)
	c.Check(snapInfo.DesktopPrefix(), Equals, "sample")

	// snap with instance key
	snapInfo.InstanceKey = "instance"
	c.Check(snapInfo.InstanceName(), Equals, "sample_instance")
	c.Check(snapInfo.Apps["app"].DesktopFile(), Matches, `.*/var/lib/snapd/desktop/applications/sample\+instance_app.desktop`)
	c.Check(snapInfo.Apps["sample"].DesktopFile(), Matches, `.*/var/lib/snapd/desktop/applications/sample\+instance_sample.desktop`)
	c.Check(snapInfo.DesktopPrefix(), Equals, "sample+instance")
}

const coreSnapYaml = `name: core
version: 0
type: os
plugs:
  network-bind:
  core-support:
`

// reading snap via ReadInfoFromSnapFile renames clashing core plugs
func (s *infoSuite) TestReadInfoFromSnapFileRenamesCorePlus(c *C) {
	snapPath := snaptest.MakeTestSnapWithFiles(c, coreSnapYaml, nil)

	snapf, err := snapfile.Open(snapPath)
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
	snaptest.MockSnap(c, coreSnapYaml, si)
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

	// snap with instance
	info.InstanceKey = "instance"
	svcNames = []string{}
	for i := range info.Services() {
		svcNames = append(svcNames, svcs[i].ServiceName())
	}
	sort.Strings(svcNames)
	c.Check(svcNames, DeepEquals, []string{
		"snap.pans_instance.svc1.service",
		"snap.pans_instance.svc2.service",
	})
}

func (s *infoSuite) TestAppInfoIsService(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: pans
apps:
  svc1:
    daemon: potato
  svc2:
    daemon: no
  svc3:
    daemon: simple
    daemon-scope: user
  app1:
  app2:
`))
	c.Assert(err, IsNil)

	svc := info.Apps["svc1"]
	c.Check(svc.IsService(), Equals, true)
	c.Check(svc.DaemonScope, Equals, snap.SystemDaemon)
	c.Check(svc.ServiceName(), Equals, "snap.pans.svc1.service")
	c.Check(svc.ServiceFile(), Equals, dirs.GlobalRootDir+"/etc/systemd/system/snap.pans.svc1.service")

	c.Check(info.Apps["svc2"].IsService(), Equals, true)
	userSvc := info.Apps["svc3"]
	c.Check(userSvc.IsService(), Equals, true)
	c.Check(userSvc.DaemonScope, Equals, snap.UserDaemon)
	c.Check(userSvc.ServiceName(), Equals, "snap.pans.svc3.service")
	c.Check(userSvc.ServiceFile(), Equals, dirs.GlobalRootDir+"/etc/systemd/user/snap.pans.svc3.service")
	c.Check(info.Apps["app1"].IsService(), Equals, false)
	c.Check(info.Apps["app1"].IsService(), Equals, false)

	// snap with instance key
	info.InstanceKey = "instance"
	c.Check(svc.ServiceName(), Equals, "snap.pans_instance.svc1.service")
	c.Check(svc.ServiceFile(), Equals, dirs.GlobalRootDir+"/etc/systemd/system/snap.pans_instance.svc1.service")
	c.Check(userSvc.ServiceName(), Equals, "snap.pans_instance.svc3.service")
	c.Check(userSvc.ServiceFile(), Equals, dirs.GlobalRootDir+"/etc/systemd/user/snap.pans_instance.svc3.service")
}

func (s *infoSuite) TestAppInfoStringer(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: asnap
apps:
  one:
   daemon: simple
`))
	c.Assert(err, IsNil)
	c.Check(fmt.Sprintf("%q", info.Apps["one"].String()), Equals, `"asnap.one"`)
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

	// snap with instance key
	info.InstanceKey = "instance"
	c.Check(socket.File(), Equals, dirs.GlobalRootDir+"/etc/systemd/system/snap.pans_instance.app1.sock1.socket")
}

func (s *infoSuite) TestTimerFile(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: pans
apps:
  app1:
    daemon: true
    timer: mon,10:00-12:00
`))

	c.Assert(err, IsNil)

	app := info.Apps["app1"]
	timerFile := app.Timer.File()
	c.Check(timerFile, Equals, dirs.GlobalRootDir+"/etc/systemd/system/snap.pans.app1.timer")
	c.Check(strings.TrimSuffix(app.ServiceFile(), ".service")+".timer", Equals, timerFile)

	// snap with instance key
	info.InstanceKey = "instance"
	c.Check(app.Timer.File(), Equals, dirs.GlobalRootDir+"/etc/systemd/system/snap.pans_instance.app1.timer")
}

func (s *infoSuite) TestLayoutParsing(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: layout-demo
layout:
  /usr:
    bind: $SNAP/usr
  /mytmp:
    type: tmpfs
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
		User:  "root",
		Group: "root",
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
	var val string
	var intVal int

	plug := &snap.PlugInfo{Snap: &snap.Info{SuggestedName: "snap"}, Name: "plug", Interface: "interface", Attrs: map[string]interface{}{"key": "value", "number": int(123)}}
	c.Assert(plug.Attr("key", &val), IsNil)
	c.Check(val, Equals, "value")

	c.Assert(plug.Attr("number", &intVal), IsNil)
	c.Check(intVal, Equals, 123)

	c.Check(plug.Attr("key", &intVal), ErrorMatches, `snap "snap" has interface "interface" with invalid value type for "key" attribute`)
	c.Check(plug.Attr("unknown", &val), ErrorMatches, `snap "snap" does not have attribute "unknown" for interface "interface"`)
	c.Check(plug.Attr("key", intVal), ErrorMatches, `internal error: cannot get "key" attribute of interface "interface" with non-pointer value`)
}

func (s *infoSuite) TestSlotInfoAttr(c *C) {
	var val string
	var intVal int

	slot := &snap.SlotInfo{Snap: &snap.Info{SuggestedName: "snap"}, Name: "plug", Interface: "interface", Attrs: map[string]interface{}{"key": "value", "number": int(123)}}

	c.Assert(slot.Attr("key", &val), IsNil)
	c.Check(val, Equals, "value")

	c.Assert(slot.Attr("number", &intVal), IsNil)
	c.Check(intVal, Equals, 123)

	c.Check(slot.Attr("key", &intVal), ErrorMatches, `snap "snap" has interface "interface" with invalid value type for "key" attribute`)
	c.Check(slot.Attr("unknown", &val), ErrorMatches, `snap "snap" does not have attribute "unknown" for interface "interface"`)
	c.Check(slot.Attr("key", intVal), ErrorMatches, `internal error: cannot get "key" attribute of interface "interface" with non-pointer value`)
}

func (s *infoSuite) TestDottedPathSlot(c *C) {
	attrs := map[string]interface{}{
		"nested": map[string]interface{}{
			"foo": "bar",
		},
	}

	slot := &snap.SlotInfo{Attrs: attrs}
	c.Assert(slot, NotNil)

	v, ok := slot.Lookup("nested.foo")
	c.Assert(ok, Equals, true)
	c.Assert(v, Equals, "bar")

	v, ok = slot.Lookup("nested")
	c.Assert(ok, Equals, true)
	c.Assert(v, DeepEquals, map[string]interface{}{
		"foo": "bar",
	})

	_, ok = slot.Lookup("x")
	c.Assert(ok, Equals, false)

	_, ok = slot.Lookup("..")
	c.Assert(ok, Equals, false)

	_, ok = slot.Lookup("nested.foo.x")
	c.Assert(ok, Equals, false)

	_, ok = slot.Lookup("nested.x")
	c.Assert(ok, Equals, false)
}

func (s *infoSuite) TestDottedPathPlug(c *C) {
	attrs := map[string]interface{}{
		"nested": map[string]interface{}{
			"foo": "bar",
		},
	}

	plug := &snap.PlugInfo{Attrs: attrs}
	c.Assert(plug, NotNil)

	v, ok := plug.Lookup("nested")
	c.Assert(ok, Equals, true)
	c.Assert(v, DeepEquals, map[string]interface{}{
		"foo": "bar",
	})

	v, ok = plug.Lookup("nested.foo")
	c.Assert(ok, Equals, true)
	c.Assert(v, Equals, "bar")

	_, ok = plug.Lookup("x")
	c.Assert(ok, Equals, false)

	_, ok = plug.Lookup("..")
	c.Assert(ok, Equals, false)

	_, ok = plug.Lookup("nested.foo.x")
	c.Assert(ok, Equals, false)
}

func (s *infoSuite) TestDefaultContentProviders(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(yamlNeedDf))
	c.Assert(err, IsNil)

	plugs := make([]*snap.PlugInfo, 0, len(info.Plugs))
	for _, plug := range info.Plugs {
		plugs = append(plugs, plug)
	}

	dps := snap.DefaultContentProviders(plugs)
	c.Check(dps, DeepEquals, map[string][]string{"gtk-common-themes": {"gtk-3-themes", "icon-themes"}})
}

func (s *infoSuite) TestExpandSnapVariables(c *C) {
	dirs.SetRootDir("")
	info, err := snap.InfoFromSnapYaml([]byte(`name: foo`))
	c.Assert(err, IsNil)
	info.Revision = snap.R(42)
	c.Assert(info.ExpandSnapVariables("$SNAP/stuff"), Equals, "/snap/foo/42/stuff")
	c.Assert(info.ExpandSnapVariables("$SNAP_DATA/stuff"), Equals, "/var/snap/foo/42/stuff")
	c.Assert(info.ExpandSnapVariables("$SNAP_COMMON/stuff"), Equals, "/var/snap/foo/common/stuff")
	c.Assert(info.ExpandSnapVariables("$GARBAGE/rocks"), Equals, "/rocks")

	info.InstanceKey = "instance"
	// Despite setting the instance key the variables expand to the same
	// value as before. This is because they are used from inside the mount
	// namespace of the instantiated snap where the mount backend will
	// ensure that the regular (non-instance) paths contain
	// instance-specific code and data.
	c.Assert(info.ExpandSnapVariables("$SNAP/stuff"), Equals, "/snap/foo/42/stuff")
	c.Assert(info.ExpandSnapVariables("$SNAP_DATA/stuff"), Equals, "/var/snap/foo/42/stuff")
	c.Assert(info.ExpandSnapVariables("$SNAP_COMMON/stuff"), Equals, "/var/snap/foo/common/stuff")
	c.Assert(info.ExpandSnapVariables("$GARBAGE/rocks"), Equals, "/rocks")
}

func (s *infoSuite) TestStopModeTypeKillMode(c *C) {
	for _, t := range []struct {
		stopMode string
		killall  bool
	}{
		{"", true},
		{"sigterm", false},
		{"sigterm-all", true},
		{"sighup", false},
		{"sighup-all", true},
		{"sigusr1", false},
		{"sigusr1-all", true},
		{"sigusr2", false},
		{"sigusr2-all", true},
	} {
		c.Check(snap.StopModeType(t.stopMode).KillAll(), Equals, t.killall, Commentf("wrong KillAll for %v", t.stopMode))
	}
}

func (s *infoSuite) TestStopModeTypeKillSignal(c *C) {
	for _, t := range []struct {
		stopMode string
		killSig  string
	}{
		{"", ""},
		{"sigterm", "SIGTERM"},
		{"sigterm-all", "SIGTERM"},
		{"sighup", "SIGHUP"},
		{"sighup-all", "SIGHUP"},
		{"sigusr1", "SIGUSR1"},
		{"sigusr1-all", "SIGUSR1"},
		{"sigusr2", "SIGUSR2"},
		{"sigusr2-all", "SIGUSR2"},
	} {
		c.Check(snap.StopModeType(t.stopMode).KillSignal(), Equals, t.killSig)
	}
}

func (s *infoSuite) TestSplitInstanceName(c *C) {
	snapName, instanceKey := snap.SplitInstanceName("foo_bar")
	c.Check(snapName, Equals, "foo")
	c.Check(instanceKey, Equals, "bar")

	snapName, instanceKey = snap.SplitInstanceName("foo")
	c.Check(snapName, Equals, "foo")
	c.Check(instanceKey, Equals, "")

	// all following instance names are invalid

	snapName, instanceKey = snap.SplitInstanceName("_bar")
	c.Check(snapName, Equals, "")
	c.Check(instanceKey, Equals, "bar")

	snapName, instanceKey = snap.SplitInstanceName("foo___bar_bar")
	c.Check(snapName, Equals, "foo")
	c.Check(instanceKey, Equals, "__bar_bar")

	snapName, instanceKey = snap.SplitInstanceName("")
	c.Check(snapName, Equals, "")
	c.Check(instanceKey, Equals, "")
}

func (s *infoSuite) TestInstanceSnapName(c *C) {
	c.Check(snap.InstanceSnap("foo_bar"), Equals, "foo")
	c.Check(snap.InstanceSnap("foo"), Equals, "foo")

	c.Check(snap.InstanceName("foo", "bar"), Equals, "foo_bar")
	c.Check(snap.InstanceName("foo", ""), Equals, "foo")
}

func (s *infoSuite) TestInstanceNameInSnapInfo(c *C) {
	info := &snap.Info{
		SuggestedName: "snap-name",
		InstanceKey:   "foo",
	}

	c.Check(info.InstanceName(), Equals, "snap-name_foo")
	c.Check(info.SnapName(), Equals, "snap-name")

	info.InstanceKey = ""
	c.Check(info.InstanceName(), Equals, "snap-name")
	c.Check(info.SnapName(), Equals, "snap-name")
}

func (s *infoSuite) TestIsActive(c *C) {
	info1 := snaptest.MockSnap(c, sampleYaml, &snap.SideInfo{Revision: snap.R(1)})
	info2 := snaptest.MockSnap(c, sampleYaml, &snap.SideInfo{Revision: snap.R(2)})
	// no current -> not active
	c.Check(info1.IsActive(), Equals, false)
	c.Check(info2.IsActive(), Equals, false)

	mountdir := info1.MountDir()
	dir, rev := filepath.Split(mountdir)
	c.Assert(os.MkdirAll(dir, 0755), IsNil)
	cur := filepath.Join(dir, "current")
	c.Assert(os.Symlink(rev, cur), IsNil)

	// is current -> is active
	c.Check(info1.IsActive(), Equals, true)
	c.Check(info2.IsActive(), Equals, false)
}

func (s *infoSuite) TestInfoTypeSnapdBackwardCompatibility(c *C) {
	const snapdYaml = `
name: snapd
type: app
version: 1
`
	snapInfo := snaptest.MockSnap(c, snapdYaml, &snap.SideInfo{Revision: snap.R(1), SnapID: "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4"})
	c.Check(snapInfo.Type(), Equals, snap.TypeSnapd)
}

func (s *infoSuite) TestDirAndFileHelpers(c *C) {
	dirs.SetRootDir("")

	c.Check(snap.MountDir("name", snap.R(1)), Equals, fmt.Sprintf("%s/name/1", dirs.SnapMountDir))
	c.Check(snap.MountFile("name", snap.R(1)), Equals, "/var/lib/snapd/snaps/name_1.snap")
	c.Check(snap.HooksDir("name", snap.R(1)), Equals, fmt.Sprintf("%s/name/1/meta/hooks", dirs.SnapMountDir))
	c.Check(snap.DataDir("name", snap.R(1)), Equals, "/var/snap/name/1")
	c.Check(snap.CommonDataDir("name"), Equals, "/var/snap/name/common")
	c.Check(snap.UserDataDir("/home/bob", "name", snap.R(1), nil), Equals, "/home/bob/snap/name/1")
	c.Check(snap.UserCommonDataDir("/home/bob", "name", nil), Equals, "/home/bob/snap/name/common")
	c.Check(snap.UserXdgRuntimeDir(12345, "name"), Equals, "/run/user/12345/snap.name")
	c.Check(snap.UserSnapDir("/home/bob", "name", nil), Equals, "/home/bob/snap/name")

	c.Check(snap.MountDir("name_instance", snap.R(1)), Equals, fmt.Sprintf("%s/name_instance/1", dirs.SnapMountDir))
	c.Check(snap.MountFile("name_instance", snap.R(1)), Equals, "/var/lib/snapd/snaps/name_instance_1.snap")
	c.Check(snap.HooksDir("name_instance", snap.R(1)), Equals, fmt.Sprintf("%s/name_instance/1/meta/hooks", dirs.SnapMountDir))
	c.Check(snap.DataDir("name_instance", snap.R(1)), Equals, "/var/snap/name_instance/1")
	c.Check(snap.CommonDataDir("name_instance"), Equals, "/var/snap/name_instance/common")
	c.Check(snap.UserDataDir("/home/bob", "name_instance", snap.R(1), nil), Equals, "/home/bob/snap/name_instance/1")
	c.Check(snap.UserCommonDataDir("/home/bob", "name_instance", nil), Equals, "/home/bob/snap/name_instance/common")
	c.Check(snap.UserXdgRuntimeDir(12345, "name_instance"), Equals, "/run/user/12345/snap.name_instance")
	c.Check(snap.UserSnapDir("/home/bob", "name_instance", nil), Equals, "/home/bob/snap/name_instance")
}

func (s *infoSuite) TestSortByType(c *C) {
	infos := []*snap.Info{
		{SuggestedName: "app1", SnapType: "app"},
		{SuggestedName: "os1", SnapType: "os"},
		{SuggestedName: "base1", SnapType: "base"},
		{SuggestedName: "gadget1", SnapType: "gadget"},
		{SuggestedName: "kernel1", SnapType: "kernel"},
		{SuggestedName: "app2", SnapType: "app"},
		{SuggestedName: "os2", SnapType: "os"},
		{SuggestedName: "snapd", SnapType: "snapd"},
		{SuggestedName: "base2", SnapType: "base"},
		{SuggestedName: "gadget2", SnapType: "gadget"},
		{SuggestedName: "kernel2", SnapType: "kernel"},
	}
	sort.Stable(snap.ByType(infos))

	c.Check(infos, DeepEquals, []*snap.Info{
		{SuggestedName: "snapd", SnapType: "snapd"},
		{SuggestedName: "os1", SnapType: "os"},
		{SuggestedName: "os2", SnapType: "os"},
		{SuggestedName: "kernel1", SnapType: "kernel"},
		{SuggestedName: "kernel2", SnapType: "kernel"},
		{SuggestedName: "base1", SnapType: "base"},
		{SuggestedName: "base2", SnapType: "base"},
		{SuggestedName: "gadget1", SnapType: "gadget"},
		{SuggestedName: "gadget2", SnapType: "gadget"},
		{SuggestedName: "app1", SnapType: "app"},
		{SuggestedName: "app2", SnapType: "app"},
	})
}

func (s *infoSuite) TestSortByTypeAgain(c *C) {
	core := &snap.Info{SnapType: snap.TypeOS}
	base := &snap.Info{SnapType: snap.TypeBase}
	app := &snap.Info{SnapType: snap.TypeApp}
	snapd := &snap.Info{}
	snapd.SideInfo = snap.SideInfo{RealName: "snapd"}

	byType := func(snaps ...*snap.Info) []*snap.Info {
		sort.Stable(snap.ByType(snaps))
		return snaps
	}

	c.Check(byType(base, core), DeepEquals, []*snap.Info{core, base})
	c.Check(byType(app, core), DeepEquals, []*snap.Info{core, app})
	c.Check(byType(app, base), DeepEquals, []*snap.Info{base, app})
	c.Check(byType(app, base, core), DeepEquals, []*snap.Info{core, base, app})
	c.Check(byType(app, core, base), DeepEquals, []*snap.Info{core, base, app})

	c.Check(byType(app, core, base, snapd), DeepEquals, []*snap.Info{snapd, core, base, app})
	c.Check(byType(app, snapd, core, base), DeepEquals, []*snap.Info{snapd, core, base, app})
}

func (s *infoSuite) TestMedia(c *C) {
	c.Check(snap.MediaInfos{}.IconURL(), Equals, "")

	media := snap.MediaInfos{
		{
			Type: "screenshot",
			URL:  "https://example.com/shot1.svg",
		}, {
			Type: "icon",
			URL:  "https://example.com/icon.png",
		}, {
			Type:   "screenshot",
			URL:    "https://example.com/shot2.svg",
			Width:  42,
			Height: 17,
		},
	}

	c.Check(media.IconURL(), Equals, "https://example.com/icon.png")
}

func (s *infoSuite) TestSortApps(c *C) {
	tcs := []struct {
		err    string
		apps   []*snap.AppInfo
		sorted []string
	}{{
		apps: []*snap.AppInfo{
			{Name: "bar", Before: []string{"baz"}},
			{Name: "foo"},
		},
		sorted: []string{"bar", "foo"},
	}, {
		apps: []*snap.AppInfo{
			{Name: "bar", Before: []string{"foo"}},
			{Name: "foo", Before: []string{"baz"}},
		},
		sorted: []string{"bar", "foo"},
	}, {
		apps: []*snap.AppInfo{
			{Name: "bar", Before: []string{"foo"}},
		},
		sorted: []string{"bar"},
	}, {
		apps: []*snap.AppInfo{
			{Name: "bar", After: []string{"foo"}},
		},
		sorted: []string{"bar"},
	}, {
		apps: []*snap.AppInfo{
			{Name: "bar", Before: []string{"baz"}},
			{Name: "baz", After: []string{"bar", "foo"}},
			{Name: "foo"},
		},
		sorted: []string{"bar", "foo", "baz"},
	}, {
		apps: []*snap.AppInfo{
			{Name: "foo", After: []string{"bar", "zed"}},
			{Name: "bar", Before: []string{"foo"}},
			{Name: "baz", After: []string{"foo"}},
			{Name: "zed"},
		},
		sorted: []string{"bar", "zed", "foo", "baz"},
	}, {
		apps: []*snap.AppInfo{
			{Name: "foo", After: []string{"baz"}},
			{Name: "bar", Before: []string{"baz"}},
			{Name: "baz"},
			{Name: "zed", After: []string{"foo", "bar", "baz"}},
		},
		sorted: []string{"bar", "baz", "foo", "zed"},
	}, {
		apps: []*snap.AppInfo{
			{Name: "foo", Before: []string{"bar"}, After: []string{"zed"}},
			{Name: "bar", Before: []string{"baz"}},
			{Name: "baz", Before: []string{"zed"}},
			{Name: "zed"},
		},
		err: `applications are part of a before/after cycle: ((foo|bar|baz|zed)(, )?){4}`,
	}, {
		apps: []*snap.AppInfo{
			{Name: "foo", Before: []string{"bar"}},
			{Name: "bar", Before: []string{"foo"}},
			{Name: "baz", Before: []string{"foo"}, After: []string{"bar"}},
		},
		err: `applications are part of a before/after cycle: ((foo|bar|baz)(, )?){3}`,
	}, {
		apps: []*snap.AppInfo{
			{Name: "baz", After: []string{"bar"}},
			{Name: "foo"},
			{Name: "bar", After: []string{"foo"}},
		},
		sorted: []string{"foo", "bar", "baz"},
	}}
	for _, tc := range tcs {
		sorted, err := snap.SortServices(tc.apps)
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err)
		} else {
			c.Assert(err, IsNil)
			c.Assert(sorted, HasLen, len(tc.sorted))
			sortedNames := make([]string, len(sorted))
			for i, app := range sorted {
				sortedNames[i] = app.Name
			}
			c.Assert(sortedNames, DeepEquals, tc.sorted)
		}
	}
}

func (s *infoSuite) TestSortAppInfoBySnapApp(c *C) {
	snap1 := &snap.Info{SuggestedName: "snapa"}
	snap2 := &snap.Info{SuggestedName: "snapb"}
	infos := []*snap.AppInfo{
		{Snap: snap1, Name: "b"},
		{Snap: snap2, Name: "b"},
		{Snap: snap1, Name: "a"},
		{Snap: snap2, Name: "a"},
	}
	sort.Stable(snap.AppInfoBySnapApp(infos))

	c.Check(infos, DeepEquals, []*snap.AppInfo{
		{Snap: snap1, Name: "a"},
		{Snap: snap1, Name: "b"},
		{Snap: snap2, Name: "a"},
		{Snap: snap2, Name: "b"},
	})
}

func (s *infoSuite) TestHelpersWithHiddenSnapFolder(c *C) {
	dirs.SetRootDir("")
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}

	c.Check(snap.UserDataDir("/home/bob", "name", snap.R(1), opts), Equals, "/home/bob/.snap/data/name/1")
	c.Check(snap.UserCommonDataDir("/home/bob", "name", opts), Equals, "/home/bob/.snap/data/name/common")
	c.Check(snap.UserSnapDir("/home/bob", "name", opts), Equals, "/home/bob/.snap/data/name")
	c.Check(snap.SnapDir("/home/bob", opts), Equals, "/home/bob/.snap/data")

	c.Check(snap.UserDataDir("/home/bob", "name_instance", snap.R(1), opts), Equals, "/home/bob/.snap/data/name_instance/1")
	c.Check(snap.UserCommonDataDir("/home/bob", "name_instance", opts), Equals, "/home/bob/.snap/data/name_instance/common")
	c.Check(snap.UserSnapDir("/home/bob", "name_instance", opts), Equals, "/home/bob/.snap/data/name_instance")
}
