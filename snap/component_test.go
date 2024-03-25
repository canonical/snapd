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
	"path/filepath"
	"strings"

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
`
	compName := "mysnap+test-info"
	testComp := snaptest.MakeTestComponentWithFiles(c, compName+".comp", componentYaml, nil)

	compf, err := snapfile.Open(testComp)
	c.Assert(err, IsNil)

	ci, err := snap.ReadComponentInfoFromContainer(compf)
	c.Assert(err, IsNil)
	c.Assert(ci, DeepEquals, snap.NewComponentInfo(
		naming.NewComponentRef("mysnap", "test-info"),
		"test",
		"1.0",
		"short description",
		"long description",
	))
	c.Assert(ci.FullName(), Equals, compName)
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

	ci, err := snap.ReadComponentInfoFromContainer(compf)
	c.Assert(err, IsNil)
	c.Assert(ci, DeepEquals, snap.NewComponentInfo(
		naming.NewComponentRef("mysnap", "test-info"),
		"test", "1.0.2", "", "",
	))
	c.Assert(ci.FullName(), Equals, compName)
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

	ci, err := snap.ReadComponentInfoFromContainer(compf)
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

	ci, err := snap.ReadComponentInfoFromContainer(compf)
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

		ci, err := snap.ReadComponentInfoFromContainer(compf)
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

	ci, err := snap.ReadComponentInfoFromContainer(compf)
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

	ci, err := snap.ReadComponentInfoFromContainer(compf)
	c.Assert(err.Error(), Equals, `cannot parse component.yaml: unknown component type "unknowntype"`)
	c.Assert(ci, IsNil)
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

		ci, err := snap.ReadComponentInfoFromContainer(compf)
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

		ci, err := snap.ReadComponentInfoFromContainer(compf)
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

	ci, err := snap.ReadComponentInfoFromContainer(compf)
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

	ci, err := snap.ReadComponentInfoFromContainer(compf)
	c.Assert(err, ErrorMatches, "description can have up to 4096 codepoints, got 4098")
	c.Assert(ci, IsNil)
}

func (s *componentSuite) TestComponentContainerPlaceInfoImpl(c *C) {
	cpi := snap.MinimalComponentContainerPlaceInfo("test-info", snap.R(25), "mysnap_instance", snap.R(11))

	var contPi snap.ContainerPlaceInfo = cpi

	c.Check(contPi.ContainerName(), Equals, "mysnap_instance+test-info")
	c.Check(contPi.Filename(), Equals, "mysnap_instance+test-info_25.comp")
	c.Check(contPi.MountDir(), Equals,
		filepath.Join(dirs.SnapMountDir, "mysnap_instance/components/11/test-info_25"))
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
