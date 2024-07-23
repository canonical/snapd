/*
 * Copyright (C) 2023 Canonical Ltd
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
	"os"
	"path/filepath"
	"strings"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type componentSuite struct {
	testutil.BaseTest
}

var _ = Suite(&componentSuite{})

func (s *componentSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	dirs.SetRootDir(c.MkDir())
}

func (s *componentSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")
}

func (s *componentSuite) TestReadComponentInfoFromFile(c *C) {
	const componentYaml = `component: mysnap+test-info
type: test
version: 1.0
summary: short description
description: long description
provenance: prov
`
	compName := "mysnap+test-info"
	testComp := snaptest.MakeTestComponentWithFiles(c, compName+".comp", componentYaml, nil)

	compf, err := snapfile.Open(testComp)
	c.Assert(err, IsNil)

	ci, err := snap.ReadComponentInfoFromContainer(compf, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(ci, DeepEquals, snap.NewComponentInfo(
		naming.NewComponentRef("mysnap", "test-info"),
		snap.ComponentType("test"),
		"1.0",
		"short description",
		"long description",
		"prov",
		nil,
	))
	c.Assert(ci.FullName(), Equals, compName)
	c.Assert(ci.Provenance(), Equals, "prov")

	// since we didn't pass a side info, then ComponentSideInfo should be empty
	c.Assert(ci.ComponentSideInfo, Equals, snap.ComponentSideInfo{})

	// since we didn't pass a snap.Info, then Hooks should be empty
	c.Assert(ci.Hooks, HasLen, 0)
}

func (s *componentSuite) TestReadComponentInfoMinimal(c *C) {
	const componentYaml = `component: mysnap+test-info
type: test
version: 1.0.2
`
	compName := "mysnap+test-info"
	testComp := snaptest.MakeTestComponentWithFiles(c, compName+".comp", componentYaml, nil)

	compf, err := snapfile.Open(testComp)
	c.Assert(err, IsNil)

	ci, err := snap.ReadComponentInfoFromContainer(compf, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(ci, DeepEquals, snap.NewComponentInfo(
		naming.NewComponentRef("mysnap", "test-info"),
		snap.ComponentType("test"),
		"1.0.2",
		"", "", "", nil,
	))
	c.Assert(ci.FullName(), Equals, compName)
	c.Assert(ci.Provenance(), Equals, naming.DefaultProvenance)
}

func (s *componentSuite) TestReadComponentInfoFromFileBadName(c *C) {
	const componentYaml = `component: mysnap-test-info
type: test
version: 1.0
summary: short description
description: long description
`
	testComp := snaptest.MakeTestComponentWithFiles(c, "mysnap-test-info.comp", componentYaml, nil)

	compf, err := snapfile.Open(testComp)
	c.Assert(err, IsNil)

	ci, err := snap.ReadComponentInfoFromContainer(compf, nil, nil)
	c.Assert(err.Error(), Equals, `cannot parse component.yaml: incorrect component name "mysnap-test-info"`)
	c.Assert(ci, IsNil)
}

func (s *componentSuite) TestReadComponentUnexpectedField(c *C) {
	const componentYaml = `component: mysnap+extra
type: test
version: 1.0
foo: bar
`
	testComp := snaptest.MakeTestComponentWithFiles(c, "mysnap+extra.comp", componentYaml, nil)

	compf, err := snapfile.Open(testComp)
	c.Assert(err, IsNil)

	ci, err := snap.ReadComponentInfoFromContainer(compf, nil, nil)
	c.Assert(err, ErrorMatches, `.*\n.*: field foo not found in type snap.ComponentInfo`)
	c.Assert(ci, IsNil)
}

func (s *componentSuite) TestReadComponentEmptyNames(c *C) {
	const componentYamlTmpl = `component: %s
type: test
version: 1.0
`

	for _, tc := range []struct {
		name, error string
	}{
		{name: "snapname+", error: "component name cannot be empty"},
		{name: "+compname", error: "snap name for component cannot be empty"},
		{name: "+", error: "snap name for component cannot be empty"},
		{name: "", error: "snap name for component cannot be empty"},
	} {
		componentYaml := fmt.Sprintf(componentYamlTmpl, tc.name)
		testComp := snaptest.MakeTestComponentWithFiles(c, "mysnap+extra.comp", componentYaml, nil)

		compf, err := snapfile.Open(testComp)
		c.Assert(err, IsNil)

		ci, err := snap.ReadComponentInfoFromContainer(compf, nil, nil)
		c.Assert(err, ErrorMatches, tc.error)
		c.Assert(ci, IsNil)
	}
}

func (s *componentSuite) TestReadComponentEmptyType(c *C) {
	const componentYaml = `component: mysnap+extra
version: 1.0
`
	testComp := snaptest.MakeTestComponentWithFiles(c, "mysnap+extra.comp", componentYaml, nil)

	compf, err := snapfile.Open(testComp)
	c.Assert(err, IsNil)

	ci, err := snap.ReadComponentInfoFromContainer(compf, nil, nil)
	c.Assert(err.Error(), Equals, `component type cannot be empty`)
	c.Assert(ci, IsNil)
}

func (s *componentSuite) TestReadComponentBadType(c *C) {
	const componentYaml = `component: mysnap+extra
type: unknowntype
version: 1.0
`
	testComp := snaptest.MakeTestComponentWithFiles(c, "mysnap+extra.comp", componentYaml, nil)

	compf, err := snapfile.Open(testComp)
	c.Assert(err, IsNil)

	ci, err := snap.ReadComponentInfoFromContainer(compf, nil, nil)
	c.Assert(err.Error(), Equals, `cannot parse component.yaml: unknown component type "unknowntype"`)
	c.Assert(ci, IsNil)
}

func (s *componentSuite) TestReadComponentBadProvenance(c *C) {
	const componentYaml = `component: mysnap+extra
type: test
version: 1.0
provenance: invalid-prov-
`
	testComp := snaptest.MakeTestComponentWithFiles(c, "mysnap+extra.comp", componentYaml, nil)

	compf, err := snapfile.Open(testComp)
	c.Assert(err, IsNil)

	_, err = snap.ReadComponentInfoFromContainer(compf, nil, nil)
	c.Assert(err.Error(), Equals, `invalid provenance: "invalid-prov-"`)
}

func (s *componentSuite) TestReadComponentVersion(c *C) {
	const componentYamlTmpl = `component: snap+comp
type: test
version: %s
`

	for _, tc := range []struct {
		version, error string
	}{
		{version: "1.0a", error: ""},
		{version: "1.2.3.4", error: ""},
		{version: "1.2_", error: ".*must end with an ASCII alphanumeric.*"},
		{version: "0123456789012345678901234567890123456789",
			error: ".*cannot be longer than 32 characters.*"},
		// version is optional
		{version: "", error: ""},
	} {
		componentYaml := fmt.Sprintf(componentYamlTmpl, tc.version)
		testComp := snaptest.MakeTestComponentWithFiles(c, "snap+comp.comp", componentYaml, nil)

		compf, err := snapfile.Open(testComp)
		c.Assert(err, IsNil)

		ci, err := snap.ReadComponentInfoFromContainer(compf, nil, nil)
		if tc.error != "" {
			c.Check(err, ErrorMatches, tc.error)
			c.Check(ci, IsNil)
		} else {
			c.Check(err, IsNil)
			c.Check(ci.Version, Equals, tc.version)
		}
	}
}

func (s *componentSuite) TestReadComponentBadName(c *C) {
	const componentYamlTmpl = `component: %s
type: test
version: 1.0
`

	for _, tc := range []struct {
		name, error string
	}{
		{name: "name_+comp", error: `invalid snap name: "name_"`},
		{name: "name+comp_", error: `invalid snap name: "comp_"`},
	} {
		componentYaml := fmt.Sprintf(componentYamlTmpl, tc.name)
		testComp := snaptest.MakeTestComponentWithFiles(c, "snap+comp.comp", componentYaml, nil)

		compf, err := snapfile.Open(testComp)
		c.Assert(err, IsNil)

		ci, err := snap.ReadComponentInfoFromContainer(compf, nil, nil)
		c.Check(err, ErrorMatches, tc.error)
		c.Assert(ci, IsNil)
	}
}

func (s *componentSuite) TestReadComponentTooLongSummary(c *C) {
	const componentYamlTmpl = `component: snap+comp
type: test
version: 2s.0b
summary: %s
description: 👹👺👻👽👾🤖
`

	componentYaml := fmt.Sprintf(componentYamlTmpl, strings.Repeat("👾🤖", 65))
	testComp := snaptest.MakeTestComponentWithFiles(c, "snap+comp.comp", componentYaml, nil)

	compf, err := snapfile.Open(testComp)
	c.Assert(err, IsNil)

	ci, err := snap.ReadComponentInfoFromContainer(compf, nil, nil)
	c.Assert(err, ErrorMatches, "summary can have up to 128 codepoints, got 130")
	c.Assert(ci, IsNil)
}

func (s *componentSuite) TestReadComponentTooLongDescription(c *C) {
	const componentYamlTmpl = `component: snap+comp
type: test
version: 2s.0b
description: %s
`

	componentYaml := fmt.Sprintf(componentYamlTmpl, strings.Repeat("👾🤖", 2049))
	testComp := snaptest.MakeTestComponentWithFiles(c, "snap+comp.comp", componentYaml, nil)

	compf, err := snapfile.Open(testComp)
	c.Assert(err, IsNil)

	ci, err := snap.ReadComponentInfoFromContainer(compf, nil, nil)
	c.Assert(err, ErrorMatches, "description can have up to 4096 codepoints, got 4098")
	c.Assert(ci, IsNil)
}

func (s *componentSuite) TestComponentContainerPlaceInfoImpl(c *C) {
	cpi := snap.MinimalComponentContainerPlaceInfo("test-info", snap.R(25), "mysnap_instance")

	var contPi snap.ContainerPlaceInfo = cpi

	c.Check(contPi.ContainerName(), Equals, "mysnap_instance+test-info")
	c.Check(contPi.Filename(), Equals, "mysnap_instance+test-info_25.comp")
	c.Check(contPi.MountDir(), Equals,
		filepath.Join(dirs.SnapMountDir, "mysnap_instance/components/mnt/test-info/25"))
	c.Check(contPi.MountFile(), Equals,
		filepath.Join(dirs.GlobalRootDir, "var/lib/snapd/snaps/mysnap_instance+test-info_25.comp"))
	c.Check(contPi.MountDescription(), Equals, "Mount unit for mysnap_instance+test-info, revision 25")
}

func (s *componentSuite) TestComponentSideInfoEqual(c *C) {
	cref := naming.NewComponentRef("snap", "comp")
	csi := snap.NewComponentSideInfo(cref, snap.R(1))

	for _, tc := range []struct {
		csi   *snap.ComponentSideInfo
		equal bool
	}{
		{snap.NewComponentSideInfo(naming.NewComponentRef("snap", "comp"), snap.R(1)), true},
		{snap.NewComponentSideInfo(naming.NewComponentRef("other", "comp"), snap.R(1)), false},
		{snap.NewComponentSideInfo(naming.NewComponentRef("snap", "other"), snap.R(1)), false},
		{snap.NewComponentSideInfo(naming.NewComponentRef("snap", "comp"), snap.R(5)), false},
		{snap.NewComponentSideInfo(naming.NewComponentRef("", ""), snap.R(0)), false},
	} {
		c.Check(csi.Equal(tc.csi), Equals, tc.equal)
	}
}

func (s *componentSuite) TestReadComponentInfoFinishedWithSnapInfoAndComponentSideInfo(c *C) {
	restore := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {
		// remove the "*-bad" interfaces, this helps us test that sanitized plugs
		// do not end up in the plugs for any hooks in a ComponentInfo
		delete(snapInfo.Plugs, "implicit-bad")
		delete(snapInfo.Plugs, "explicit-bad")
		for _, comp := range snapInfo.Components {
			for _, hook := range comp.ExplicitHooks {
				delete(hook.Plugs, "explicit-bad")
				delete(hook.Plugs, "implicit-bad")
			}
		}
	})
	defer restore()

	const componentYaml = `component: snap+component
type: test
version: 1.0
summary: short description
description: long description
`
	const compName = "snap+component"
	testComp := snaptest.MakeTestComponentWithFiles(c, compName+".comp", componentYaml, [][]string{
		{"meta/hooks/install", "echo 'explicit hook'"},
		{"meta/hooks/pre-refresh", "echo 'implicit hook'"},
	})

	compf, err := snapfile.Open(testComp)
	c.Assert(err, IsNil)

	const snapYaml = `
name: snap
components:
  component:
    type: test
    hooks:
      install:
        plugs: [network, implicit-bad]
      remove:
plugs:
  network-client:
  explicit-bad:
`

	snapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)

	ci, err := snap.ReadComponentInfoFromContainer(compf, snapInfo, &snap.ComponentSideInfo{
		Component: naming.NewComponentRef("snap", "component"),
		Revision:  snap.R(1),
	})
	c.Assert(err, IsNil)

	c.Check(ci.Component.ComponentName, Equals, "component")
	c.Check(ci.Component.SnapName, Equals, "snap")
	c.Check(ci.Type, Equals, snap.TestComponent)
	c.Check(ci.Version, Equals, "1.0")
	c.Check(ci.Summary, Equals, "short description")
	c.Check(ci.Description, Equals, "long description")
	c.Check(ci.FullName(), Equals, compName)
	c.Check(ci.ComponentSideInfo.Component.ComponentName, Equals, "component")
	c.Check(ci.ComponentSideInfo.Component.SnapName, Equals, "snap")
	c.Check(ci.Revision, Equals, snap.R(1))

	c.Check(ci.Hooks, HasLen, 3)

	installHook := ci.Hooks["install"]
	c.Assert(installHook, NotNil)
	c.Check(installHook.Name, Equals, "install")
	c.Check(installHook.Explicit, Equals, true)
	c.Check(installHook.Plugs, HasLen, 2)
	c.Check(installHook.Plugs["network-client"], NotNil)
	c.Check(installHook.Plugs["network"], NotNil)

	// should be missing, since it was removed as a from the plugs on all
	// ExplicitHooks
	c.Check(installHook.Plugs["implicit-bad"], IsNil)
	c.Check(installHook.Plugs["explicit-bad"], IsNil)

	preRefreshHook := ci.Hooks["pre-refresh"]
	c.Assert(preRefreshHook, NotNil)
	c.Check(preRefreshHook.Name, Equals, "pre-refresh")
	c.Check(preRefreshHook.Explicit, Equals, false)
	c.Check(preRefreshHook.Plugs, HasLen, 1)
	c.Check(preRefreshHook.Plugs["network-client"], NotNil)

	removeHook := ci.Hooks["remove"]
	c.Assert(removeHook, NotNil)
	c.Check(removeHook.Name, Equals, "remove")
	c.Check(removeHook.Explicit, Equals, true)
	c.Check(removeHook.Plugs, HasLen, 1)
	c.Check(removeHook.Plugs["network-client"], NotNil)
}

func (s *componentSuite) TestReadComponentInfoAllImplicitHooks(c *C) {
	const componentYaml = `component: snap+component
type: test
version: 1.0
summary: short description
description: long description
`
	const compName = "snap+component"
	testComp := snaptest.MakeTestComponentWithFiles(c, compName+".comp", componentYaml, [][]string{
		{"meta/hooks/install", "echo 'implicit hook'"},
		{"meta/hooks/pre-refresh", "echo 'implicit hook'"},
	})

	compf, err := snapfile.Open(testComp)
	c.Assert(err, IsNil)

	const snapYaml = `
name: snap
components:
  component:
    type: test
plugs:
  network-client:
`

	snapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)

	ci, err := snap.ReadComponentInfoFromContainer(compf, snapInfo, &snap.ComponentSideInfo{
		Component: naming.NewComponentRef("snap", "component"),
		Revision:  snap.R(1),
	})
	c.Assert(err, IsNil)

	c.Check(ci.Component.ComponentName, Equals, "component")
	c.Check(ci.Component.SnapName, Equals, "snap")
	c.Check(ci.Type, Equals, snap.TestComponent)
	c.Check(ci.Version, Equals, "1.0")
	c.Check(ci.Summary, Equals, "short description")
	c.Check(ci.Description, Equals, "long description")
	c.Check(ci.FullName(), Equals, compName)
	c.Check(ci.ComponentSideInfo.Component.ComponentName, Equals, "component")
	c.Check(ci.ComponentSideInfo.Component.SnapName, Equals, "snap")
	c.Check(ci.Revision, Equals, snap.R(1))

	c.Check(ci.Hooks, HasLen, 2)

	installHook := ci.Hooks["install"]
	c.Assert(installHook, NotNil)
	c.Check(installHook.Name, Equals, "install")
	c.Check(installHook.Explicit, Equals, false)
	c.Check(installHook.Plugs, HasLen, 1)
	c.Check(installHook.Plugs["network-client"], NotNil)

	preRefreshHook := ci.Hooks["pre-refresh"]
	c.Assert(preRefreshHook, NotNil)
	c.Check(preRefreshHook.Name, Equals, "pre-refresh")
	c.Check(preRefreshHook.Explicit, Equals, false)
	c.Check(preRefreshHook.Plugs, HasLen, 1)
	c.Check(preRefreshHook.Plugs["network-client"], NotNil)
}

func (s *componentSuite) TestReadComponentInfoFinishedWithSnapInfoMissingComponentError(c *C) {
	const componentYaml = `component: snap+component
type: test
version: 1.0
summary: short description
description: long description
`
	const compName = "snap+component"
	testComp := snaptest.MakeTestComponentWithFiles(c, compName+".comp", componentYaml, nil)

	compf, err := snapfile.Open(testComp)
	c.Assert(err, IsNil)

	const snapYaml = `
name: snap
components:
  other-component:
    type: test
`

	snapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)

	_, err = snap.ReadComponentInfoFromContainer(compf, snapInfo, nil)
	c.Assert(err, ErrorMatches, `"component" is not a component for snap "snap"`)
}

func (s *componentSuite) TestReadComponentInfoFinishedWithDifferentSnapInfoError(c *C) {
	const componentYaml = `component: snap+component
type: test
version: 1.0
summary: short description
description: long description
`
	const compName = "snap+component"
	testComp := snaptest.MakeTestComponentWithFiles(c, compName+".comp", componentYaml, nil)

	compf, err := snapfile.Open(testComp)
	c.Assert(err, IsNil)

	const snapYaml = `
name: other-snap
components:
  component:
    type: test
`

	snapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)

	_, err = snap.ReadComponentInfoFromContainer(compf, snapInfo, nil)
	c.Assert(err, ErrorMatches, `component "snap\+component" is not a component for snap "other-snap"`)
}

func (s *componentSuite) TestReadComponentInfoInconsistentTypes(c *C) {
	const componentYaml = `component: snap+component
type: test
version: 1.0
summary: short description
description: long description
`
	const compName = "snap+component"
	testComp := snaptest.MakeTestComponentWithFiles(c, compName+".comp", componentYaml, nil)

	compf, err := snapfile.Open(testComp)
	c.Assert(err, IsNil)

	const snapYaml = `
name: snap
components:
  component:
    type: kernel-modules
`

	snapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)

	_, err = snap.ReadComponentInfoFromContainer(compf, snapInfo, nil)
	c.Assert(err, ErrorMatches, `inconsistent component type \("kernel-modules" in snap, "test" in component\)`)
}

func (s *componentSuite) TestHooksForPlug(c *C) {
	const snapYaml = `
name: snap
version: 1
apps:
 one:
   command: one
   plugs: [app-plug]
components:
  comp:
    type: test
    hooks:
      install:
        plugs: [scoped-plug]
      pre-refresh:
plugs:
  unscoped-plug:
  app-plug:
`
	const componentYaml = `
component: snap+comp
type: test
version: 1
`

	info := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})

	componentInfo := snaptest.MockComponent(c, componentYaml, info, snap.ComponentSideInfo{
		Revision: snap.R(1),
	})

	scoped := info.Plugs["scoped-plug"]
	c.Assert(scoped, NotNil)

	scopedHooks := componentInfo.HooksForPlug(scoped)
	c.Assert(scopedHooks, testutil.DeepUnsortedMatches, []*snap.HookInfo{componentInfo.Hooks["install"]})

	unscoped := info.Plugs["unscoped-plug"]
	c.Assert(unscoped, NotNil)

	unscopedHooks := componentInfo.HooksForPlug(unscoped)
	c.Assert(unscopedHooks, testutil.DeepUnsortedMatches, []*snap.HookInfo{componentInfo.Hooks["install"], componentInfo.Hooks["pre-refresh"]})
}

func (s *componentSuite) TestComponentLinkPath(c *C) {
	for i, tc := range []struct {
		cpi     snap.ContainerPlaceInfo
		snapRev snap.Revision
		link    string
	}{
		{snap.MinimalComponentContainerPlaceInfo("test-info", snap.R(25), "mysnap"),
			snap.R(11), "mysnap/components/11/test-info"},
		{snap.MinimalComponentContainerPlaceInfo("test-info", snap.R(33), "mysnap"),
			snap.R(11), "mysnap/components/11/test-info"},
		{snap.MinimalComponentContainerPlaceInfo("comp-1", snap.R(25), "foo"),
			snap.R(11), "foo/components/11/comp-1"},
	} {
		c.Logf("case %d, expected link %q", i, tc.link)
		c.Check(snap.ComponentLinkPath(tc.cpi, tc.snapRev), Equals,
			filepath.Join(dirs.SnapMountDir, tc.link))
	}
}

func (s *infoSuite) TestComponentInstallDate(c *C) {
	cpi := snap.MinimalComponentContainerPlaceInfo("comp", snap.R(1), "snap")

	// not current -> Zero
	c.Check(snap.ComponentInstallDate(cpi, snap.R(33)), IsNil)

	//time.Sleep(time.Second)
	link := snap.ComponentLinkPath(cpi, snap.R(33))
	dir, _ := filepath.Split(link)
	c.Assert(os.MkdirAll(dir, 0755), IsNil)
	c.Assert(os.Symlink(dirs.GlobalRootDir, link), IsNil)
	st, err := os.Lstat(link)
	c.Assert(err, IsNil)
	instTime := st.ModTime()

	installDate := snap.ComponentInstallDate(cpi, snap.R(33))
	c.Check(installDate.Equal(instTime), Equals, true)
}

func (s *infoSuite) TestComponentSize(c *C) {
	cpi := snap.MinimalComponentContainerPlaceInfo("comp", snap.R(1), "snap")
	mntFile := cpi.MountFile()
	dir, _ := filepath.Split(mntFile)
	c.Assert(os.MkdirAll(dir, 0755), IsNil)

	// No file
	compSz, err := snap.ComponentSize(cpi)
	c.Check(compSz, Equals, int64(0))
	c.Check(err, ErrorMatches, `error while looking for component file .*snap\+comp_1\.comp: no such file or directory`)

	// File
	c.Assert(os.WriteFile(mntFile, []byte{0, 0}, 0644), IsNil)
	compSz, err = snap.ComponentSize(cpi)
	c.Check(compSz, Equals, int64(2))
	c.Check(err, IsNil)

	// Special file
	c.Assert(os.Remove(mntFile), IsNil)
	c.Assert(syscall.Mkfifo(mntFile, 0666), IsNil)
	compSz, err = snap.ComponentSize(cpi)
	c.Check(compSz, Equals, int64(0))
	c.Check(err, ErrorMatches, `unexpected file type for component file .*snap\+comp_1\.comp"`)
}
