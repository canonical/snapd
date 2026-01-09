// -*- Mode: Go; indent-tabs-mode: t -*-

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

package naming_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/pack"
)

type componentRefSuite struct{}

var _ = Suite(&componentRefSuite{})

func (s *componentRefSuite) TestNewComponentRefAndString(c *C) {
	fooRef := naming.NewComponentRef("foo", "foo-comp")
	c.Check(fooRef.SnapName, Equals, "foo")
	c.Check(fooRef.ComponentName, Equals, "foo-comp")
	c.Check(fooRef.String(), Equals, "foo+foo-comp")
}

func (s *componentRefSuite) TestValidate(c *C) {
	fooRef := naming.NewComponentRef("foo", "foo-comp")
	c.Check(fooRef.Validate(), IsNil)

	fooRef = naming.NewComponentRef("foo_", "foo-comp")
	c.Check(fooRef.Validate().Error(), Equals, `invalid snap name: "foo_"`)
}

func (s *componentRefSuite) TestUnmarshal(c *C) {
	var cr naming.ComponentRef

	yamlData := []byte(`mysnap+test-info`)
	c.Check(yaml.UnmarshalStrict(yamlData, &cr), IsNil)

	yamlData = []byte(`mysnap`)
	c.Check(yaml.UnmarshalStrict(yamlData, &cr).Error(), Equals, `incorrect component name "mysnap"`)
}

func (s *componentRefSuite) TestSplitFullComponentNameOk(c *C) {
	for _, tc := range []naming.ComponentRef{
		naming.NewComponentRef("foo", "foo-comp"),
		naming.NewComponentRef("a-b_c", "d_j-p"),
		naming.NewComponentRef("_", "c"),
	} {
		snap, comp, err := naming.SplitFullComponentName(tc.String())
		c.Logf("testing %q", tc)
		c.Assert(err, IsNil)
		c.Check(snap, Equals, tc.SnapName)
		c.Check(comp, Equals, tc.ComponentName)
	}
}

func (s *componentRefSuite) TestSplitFullComponentNameErr(c *C) {
	for _, tc := range []string{
		"blah",
		"snap++comp",
		"ff-rb",
	} {
		c.Logf("testing %q", tc)
		snap, comp, err := naming.SplitFullComponentName(tc)
		c.Assert(err, NotNil)
		c.Assert(err.Error(), Equals, fmt.Sprintf("incorrect component name %q", tc))
		c.Check(snap, Equals, "")
		c.Check(comp, Equals, "")
	}
}

func (s *componentRefSuite) TestComponentRefFromSnapPackFilename(c *C) {
	type test struct {
		filename string
		cr       naming.ComponentRef
	}

	cases := []test{
		{
			filename: "foo+bar_1.0.0.comp",
			cr:       naming.NewComponentRef("foo", "bar"),
		},
		{
			filename: "foo+bar.comp",
			cr:       naming.NewComponentRef("foo", "bar"),
		},
		{
			filename: "snap-1+comp-1_1.0.0.comp",
			cr:       naming.NewComponentRef("snap-1", "comp-1"),
		},
		{
			filename: "snap-1+comp-1_v1.comp",
			cr:       naming.NewComponentRef("snap-1", "comp-1"),
		},
		{
			filename: "foo+bar_1.0.0.snap",
		},
		{
			filename: "+bar_1.0.0.comp",
		},
		{
			filename: "foo+_1.0.0.comp",
		},
	}

	for _, tc := range cases {
		cref, err := naming.ComponentRefFromSnapPackFilename(tc.filename)
		if tc.cr == (naming.ComponentRef{}) {
			c.Check(err.Error(), Equals, fmt.Sprintf("cannot parse snap pack component filename: %q", tc.filename))
			c.Check(cref, Equals, naming.ComponentRef{})
		} else {
			c.Check(err, IsNil)
			if err == nil {
				c.Check(cref, DeepEquals, tc.cr)
			}
		}
	}
}

func (s *componentRefSuite) TestComponentRefFromSnapPack(c *C) {
	root := c.MkDir()

	packDir := filepath.Join(root, "component")
	err := os.Mkdir(packDir, 0o755)
	c.Assert(err, IsNil)

	err = os.Mkdir(filepath.Join(packDir, "meta"), 0o755)
	c.Assert(err, IsNil)

	// check parsing a component with a version
	err = os.WriteFile(filepath.Join(packDir, "meta", "component.yaml"), []byte(`
component: snap+comp
type: standard
version: 1.0
`), 0644)
	c.Assert(err, IsNil)

	target := c.MkDir()
	path, err := pack.Pack(packDir, &pack.Options{TargetDir: target})
	c.Assert(err, IsNil)

	filename := filepath.Base(path)
	c.Assert(filename, Equals, "snap+comp_1.0.comp")

	cref, err := naming.ComponentRefFromSnapPackFilename(filename)
	c.Assert(err, IsNil)

	c.Check(cref, DeepEquals, naming.NewComponentRef("snap", "comp"))

	// check parsing a component without a version
	err = os.WriteFile(filepath.Join(packDir, "meta", "component.yaml"), []byte(`
component: snap+comp
type: standard
`), 0644)
	c.Assert(err, IsNil)

	path, err = pack.Pack(packDir, &pack.Options{TargetDir: target})
	c.Assert(err, IsNil)

	filename = filepath.Base(path)
	c.Assert(filename, Equals, "snap+comp.comp")

	cref, err = naming.ComponentRefFromSnapPackFilename(filename)
	c.Assert(err, IsNil)

	c.Check(cref, DeepEquals, naming.NewComponentRef("snap", "comp"))
}
