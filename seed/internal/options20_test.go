// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package internal_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/seed/internal"
)

type options20Suite struct{}

var _ = Suite(&options20Suite{})

var mockOptions20 = []byte(`
snaps:
 - name: foo
   id: snapidsnapidsnapidsnapidsnapidsn
   channel: edge
 - name: local
   unasserted: local_v1.snap
`)

func (s *options20Suite) TestSimple(c *C) {
	fn := filepath.Join(c.MkDir(), "options.yaml")
	err := os.WriteFile(fn, mockOptions20, 0644)
	c.Assert(err, IsNil)

	options20, err := internal.ReadOptions20(fn)
	c.Assert(err, IsNil)
	c.Assert(options20.Snaps, HasLen, 2)
	c.Assert(options20.Snaps[0], DeepEquals, &internal.Snap20{
		Name:    "foo",
		SnapID:  "snapidsnapidsnapidsnapidsnapidsn",
		Channel: "edge",
	})
	c.Assert(options20.Snaps[1], DeepEquals, &internal.Snap20{
		Name:       "local",
		Unasserted: "local_v1.snap",
	})
}

func (s *options20Suite) TestEmpty(c *C) {
	fn := filepath.Join(c.MkDir(), "options.yaml")
	err := os.WriteFile(fn, []byte(`
snaps:
 -
`), 0644)
	c.Assert(err, IsNil)

	_, err = internal.ReadOptions20(fn)
	c.Assert(err, ErrorMatches, `cannot read grade dangerous options yaml: empty snaps element`)
}

func (s *options20Suite) TestNoPathAllowed(c *C) {
	fn := filepath.Join(c.MkDir(), "options.yaml")
	err := os.WriteFile(fn, []byte(`
snaps:
 - name: foo
   unasserted: foo/bar.snap
`), 0644)
	c.Assert(err, IsNil)

	_, err = internal.ReadOptions20(fn)
	c.Assert(err, ErrorMatches, `cannot read grade dangerous options yaml: "foo/bar.snap" must be a filename, not a path`)
}

func (s *options20Suite) TestDuplicatedSnapName(c *C) {
	fn := filepath.Join(c.MkDir(), "options.yaml")
	err := os.WriteFile(fn, []byte(`
snaps:
 - name: foo
   channel: stable
 - name: foo
   channel: edge
`), 0644)
	c.Assert(err, IsNil)

	_, err = internal.ReadOptions20(fn)
	c.Assert(err, ErrorMatches, `cannot read grade dangerous options yaml: snap name "foo" must be unique`)
}

func (s *options20Suite) TestValidateChannelUnhappy(c *C) {
	fn := filepath.Join(c.MkDir(), "options.yaml")
	err := os.WriteFile(fn, []byte(`
snaps:
 - name: foo
   channel: invalid/channel/
`), 0644)
	c.Assert(err, IsNil)

	_, err = internal.ReadOptions20(fn)
	c.Assert(err, ErrorMatches, `cannot read grade dangerous options yaml: invalid risk in channel name: invalid/channel/`)
}

func (s *options20Suite) TestValidateSnapIDUnhappy(c *C) {
	fn := filepath.Join(c.MkDir(), "options.yaml")
	err := os.WriteFile(fn, []byte(`
snaps:
 - name: foo
   id: foo
`), 0644)
	c.Assert(err, IsNil)

	_, err = internal.ReadOptions20(fn)
	c.Assert(err, ErrorMatches, `cannot read grade dangerous options yaml: invalid snap-id: "foo"`)
}

func (s *options20Suite) TestValidateNameUnhappy(c *C) {
	fn := filepath.Join(c.MkDir(), "options.yaml")
	err := os.WriteFile(fn, []byte(`
snaps:
 - name: invalid--name
   unasserted: ./foo.snap
`), 0644)
	c.Assert(err, IsNil)

	_, err = internal.ReadOptions20(fn)
	c.Assert(err, ErrorMatches, `cannot read grade dangerous options yaml: invalid snap name: "invalid--name"`)
}

func (s *options20Suite) TestValidateNameInstanceUnsupported(c *C) {
	fn := filepath.Join(c.MkDir(), "options.yaml")
	err := os.WriteFile(fn, []byte(`
snaps:
 - name: foo_1
   unasserted: ./foo.snap
`), 0644)
	c.Assert(err, IsNil)

	_, err = internal.ReadOptions20(fn)
	c.Assert(err, ErrorMatches, `cannot read grade dangerous options yaml: invalid snap name: "foo_1"`)
}

func (s *options20Suite) TestValidateNameMissing(c *C) {
	fn := filepath.Join(c.MkDir(), "options.yaml")
	err := os.WriteFile(fn, []byte(`
snaps:
 - unasserted: ./foo.snap
`), 0644)
	c.Assert(err, IsNil)

	_, err = internal.ReadOptions20(fn)
	c.Assert(err, ErrorMatches, `cannot read grade dangerous options yaml: invalid snap name: ""`)
}

func (s *options20Suite) TestValidateOptionMissing(c *C) {
	fn := filepath.Join(c.MkDir(), "options.yaml")
	err := os.WriteFile(fn, []byte(`
snaps:
 - name: foo
`), 0644)
	c.Assert(err, IsNil)

	_, err = internal.ReadOptions20(fn)
	c.Assert(err, ErrorMatches, `cannot read grade dangerous options yaml: at least one of id, channel or unasserted must be set for snap "foo"`)
}

func (s *options20Suite) TestWithComponents(c *C) {
	fn := filepath.Join(c.MkDir(), "options.yaml")
	err := os.WriteFile(fn, []byte(`
snaps:
 - name: foo
   unasserted: bar.snap
   components:
     - name: comp1
       unasserted: comp1_1.comp
     - name: comp2
       unasserted: file.comp
`), 0644)

	c.Assert(err, IsNil)
	options20, err := internal.ReadOptions20(fn)
	c.Assert(err, IsNil)
	c.Assert(options20.Snaps, HasLen, 1)
	c.Assert(options20.Snaps[0], DeepEquals, &internal.Snap20{
		Name:       "foo",
		Unasserted: "bar.snap",
		Components: []internal.Component{
			{Name: "comp1", Unasserted: "comp1_1.comp"},
			{Name: "comp2", Unasserted: "file.comp"},
		},
	})
}

func (s *options20Suite) TestWithComponentsAssertedSnap(c *C) {
	fn := filepath.Join(c.MkDir(), "options.yaml")
	err := os.WriteFile(fn, []byte(`
snaps:
 - name: foo
   id: snapidsnapidsnapidsnapidsnapidsn
   components:
     - name: comp1
       unasserted: comp1_1.comp
`), 0644)

	c.Assert(err, IsNil)
	options20, err := internal.ReadOptions20(fn)
	c.Assert(options20, IsNil)
	c.Assert(err, ErrorMatches, `cannot read grade dangerous options yaml: local components specified for non-local snap "foo"`)
}

func (s *options20Suite) TestWithComponentsBadCompName(c *C) {
	fn := filepath.Join(c.MkDir(), "options.yaml")
	err := os.WriteFile(fn, []byte(`
snaps:
 - name: foo
   unasserted: bar.snap
   components:
     - name: comp_1
       unasserted: comp1_1.comp
`), 0644)

	c.Assert(err, IsNil)
	options20, err := internal.ReadOptions20(fn)
	c.Assert(options20, IsNil)
	c.Assert(err, ErrorMatches, `cannot read grade dangerous options yaml: invalid snap name: "comp_1"`)
}

func (s *options20Suite) TestWithComponentsNoUnassertedComp(c *C) {
	fn := filepath.Join(c.MkDir(), "options.yaml")
	err := os.WriteFile(fn, []byte(`
snaps:
 - name: foo
   unasserted: bar.snap
   components:
     - name: comp-1
`), 0644)

	c.Assert(err, IsNil)
	options20, err := internal.ReadOptions20(fn)
	c.Assert(options20, IsNil)
	c.Assert(err, ErrorMatches, `cannot read grade dangerous options yaml: no file specified for unasserted component "comp-1"`)
}

func (s *options20Suite) TestWithComponentsNoPathAllowed(c *C) {
	fn := filepath.Join(c.MkDir(), "options.yaml")
	err := os.WriteFile(fn, []byte(`
snaps:
 - name: foo
   unasserted: bar.snap
   components:
     - name: comp1
       unasserted: ./comp1_1.comp
`), 0644)

	c.Assert(err, IsNil)
	options20, err := internal.ReadOptions20(fn)
	c.Assert(options20, IsNil)
	c.Assert(err, ErrorMatches, `cannot read grade dangerous options yaml: "\./comp1_1.comp" must be a filename, not a path`)
}
