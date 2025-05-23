// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package osutil_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type profileSuite struct{}

var _ = Suite(&profileSuite{})

// Test that loading a profile from inexisting file returns an empty profile.
func (s *profileSuite) TestLoadMountProfile1(c *C) {
	dir := c.MkDir()
	p, err := osutil.LoadMountProfile(filepath.Join(dir, "missing"))
	c.Assert(err, IsNil)
	c.Assert(p.Entries, HasLen, 0)
}

// Test that loading profile from a file works as expected.
func (s *profileSuite) TestLoadMountProfile2(c *C) {
	dir := c.MkDir()
	fname := filepath.Join(dir, "existing")
	err := os.WriteFile(fname, []byte("name-1 dir-1 type-1 options-1 1 1 # 1st entry"), 0644)
	c.Assert(err, IsNil)
	p, err := osutil.LoadMountProfile(fname)
	c.Assert(err, IsNil)
	c.Assert(p.Entries, HasLen, 1)
	c.Assert(p.Entries, DeepEquals, []osutil.MountEntry{
		{Name: "name-1", Dir: "dir-1", Type: "type-1", Options: []string{"options-1"}, DumpFrequency: 1, CheckPassNumber: 1},
	})
}

// Test that loading profile with various comments works as expected.
func (s *profileSuite) TestLoadMountProfile3(c *C) {
	dir := c.MkDir()
	fname := filepath.Join(dir, "existing")
	err := os.WriteFile(fname, []byte(`
   # comment with leading spaces
name#-1 dir#-1 type#-1 options#-1 1 1 # inline comment
# comment without leading spaces


`), 0644)
	c.Assert(err, IsNil)
	p, err := osutil.LoadMountProfile(fname)
	c.Assert(err, IsNil)
	c.Assert(p.Entries, HasLen, 1)
	c.Assert(p.Entries, DeepEquals, []osutil.MountEntry{
		{Name: "name#-1", Dir: "dir#-1", Type: "type#-1", Options: []string{"options#-1"}, DumpFrequency: 1, CheckPassNumber: 1},
	})
}

func (s *profileSuite) TestLoadMountProfileText(c *C) {
	p1, err := osutil.LoadMountProfileText("tmpfs /tmp tmpfs defaults 0 0")
	c.Assert(err, IsNil)
	c.Assert(p1.Entries, DeepEquals, []osutil.MountEntry{
		{Name: "tmpfs", Dir: "/tmp", Type: "tmpfs", Options: []string{"defaults"}},
	})

	p2, err := osutil.LoadMountProfileText(
		"tmpfs /tmp tmpfs defaults 0 0\n" +
			"/tmp /var/tmp none bind 0 0\n")
	c.Assert(err, IsNil)
	c.Assert(p2.Entries, DeepEquals, []osutil.MountEntry{
		{Name: "tmpfs", Dir: "/tmp", Type: "tmpfs", Options: []string{"defaults"}},
		{Name: "/tmp", Dir: "/var/tmp", Type: "none", Options: []string{"bind"}},
	})
}

// Test that saving a profile to a file works correctly.
func (s *profileSuite) TestSaveMountProfile1(c *C) {
	dir := c.MkDir()
	fname := filepath.Join(dir, "profile")
	p := &osutil.MountProfile{
		Entries: []osutil.MountEntry{
			{Name: "name-1", Dir: "dir-1", Type: "type-1", Options: []string{"options-1"}, DumpFrequency: 1, CheckPassNumber: 1},
		},
	}
	err := osutil.SaveMountProfile(p, fname, osutil.NoChown, osutil.NoChown)
	c.Assert(err, IsNil)

	stat, err := os.Stat(fname)
	c.Assert(err, IsNil)
	c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0644))

	c.Assert(fname, testutil.FileEquals, "name-1 dir-1 type-1 options-1 1 1\n")
}

// Test that empty fstab is parsed without errors
func (s *profileSuite) TestReadMountProfile1(c *C) {
	p, err := osutil.ReadMountProfile(strings.NewReader(""))
	c.Assert(err, IsNil)
	c.Assert(p.Entries, HasLen, 0)
}

// Test that '#'-comments are skipped
func (s *profileSuite) TestReadMountProfile2(c *C) {
	p, err := osutil.ReadMountProfile(strings.NewReader("# comment"))
	c.Assert(err, IsNil)
	c.Assert(p.Entries, HasLen, 0)
}

// Test that simple profile can be loaded correctly.
func (s *profileSuite) TestReadMountProfile3(c *C) {
	p, err := osutil.ReadMountProfile(strings.NewReader(`
		name-1 dir-1 type-1 options-1 1 1 # 1st entry
		name-2 dir-2 type-2 options-2 2 2 # 2nd entry`))
	c.Assert(err, IsNil)
	c.Assert(p.Entries, HasLen, 2)
	c.Assert(p.Entries, DeepEquals, []osutil.MountEntry{
		{Name: "name-1", Dir: "dir-1", Type: "type-1", Options: []string{"options-1"}, DumpFrequency: 1, CheckPassNumber: 1},
		{Name: "name-2", Dir: "dir-2", Type: "type-2", Options: []string{"options-2"}, DumpFrequency: 2, CheckPassNumber: 2},
	})
}

// Test that writing an empty fstab file works correctly.
func (s *profileSuite) TestWriteTo1(c *C) {
	p := &osutil.MountProfile{}
	var buf bytes.Buffer
	n, err := p.WriteTo(&buf)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, int64(0))
	c.Assert(buf.String(), Equals, "")
}

// Test that writing an trivial fstab file works correctly.
func (s *profileSuite) TestWriteTo2(c *C) {
	p := &osutil.MountProfile{
		Entries: []osutil.MountEntry{
			{Name: "name-1", Dir: "dir-1", Type: "type-1", Options: []string{"options-1"}, DumpFrequency: 1, CheckPassNumber: 1},
			{Name: "name-2", Dir: "dir-2", Type: "type-2", Options: []string{"options-2"}, DumpFrequency: 2, CheckPassNumber: 2},
		},
	}
	var buf bytes.Buffer
	n, err := p.WriteTo(&buf)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, int64(68))
	c.Assert(buf.String(), Equals, ("" +
		"name-1 dir-1 type-1 options-1 1 1\n" +
		"name-2 dir-2 type-2 options-2 2 2\n"))
}
