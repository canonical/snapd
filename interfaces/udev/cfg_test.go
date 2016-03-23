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

package udev_test

import (
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/interfaces/udev"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
)

type cfgSuite struct {
	cfg      interfaces.SecurityBackend
	snapInfo *snap.Info
	appInfo  *snap.AppInfo
}

var _ = Suite(&cfgSuite{cfg: &udev.Configurator{}})

func (s *cfgSuite) SetUpTest(c *C) {
	snapInfo, err := snap.InfoFromSnapYaml([]byte(`
name: SNAP
apps:
    APP:
`))
	c.Assert(err, IsNil)
	s.snapInfo = snapInfo
	s.appInfo = snapInfo.Apps["APP"]
}

// Tests for Configurator.SecuritySystem()

func (s *cfgSuite) TestSecuritySystem(c *C) {
	c.Assert(s.cfg.SecuritySystem(), Equals, interfaces.SecurityUDev)
}

// Tests for Configurator.DirStateForInstalledSnap()

func (s *cfgSuite) TestDirStateForInstalledSnap(c *C) {
	for _, scenario := range []struct {
		developerMode bool
		snippets      map[string][][]byte
		content       map[string]*osutil.FileState
	}{
		{}, // no snippets, no content
		{
			snippets: map[string][][]byte{
				"APP": {[]byte("snippet1"), []byte("snippet2")},
			},
			content: map[string]*osutil.FileState{
				"70-snap.SNAP.APP.rules": {
					Content: []byte("snippet1\nsnippet2"),
					Mode:    0644,
				},
			},
		},
		{developerMode: true},
		{
			developerMode: true,
			snippets: map[string][][]byte{
				"APP": {[]byte("snippet1"), []byte("snippet2")},
			},
			content: map[string]*osutil.FileState{
				"70-snap.SNAP.APP.rules": {
					Content: []byte("snippet1\nsnippet2"),
					Mode:    0644,
				},
			},
		},
	} {
		dir, glob, content, err := s.cfg.DirStateForInstalledSnap(s.snapInfo, scenario.developerMode, scenario.snippets)
		c.Assert(err, IsNil)
		c.Check(dir, Equals, udev.Directory())
		c.Check(glob, Equals, "70-snap.SNAP.*.rules")
		c.Check(content, DeepEquals, scenario.content)
		// Sanity checking as required by osutil.EnsureDirState()
		for name := range content {
			// Ensure that the file name matches the returned glob.
			matched, err := filepath.Match(glob, name)
			c.Assert(err, IsNil)
			c.Check(matched, Equals, true)
			// Ensure that the file name has no directory component
			c.Check(filepath.Base(name), Equals, name)
		}
		// Ensure that the file name will be picked up by udev.
		for name := range content {
			c.Check(strings.HasSuffix(name, ".rules"), Equals, true)
		}
	}
}

// Tests for Configurator.DirStateForRemovedSnap()

func (s *cfgSuite) TestDirStateForRemovedSnap(c *C) {
	dir, glob := s.cfg.DirStateForRemovedSnap(s.snapInfo)
	c.Check(dir, Equals, udev.Directory())
	c.Check(glob, Equals, "70-snap.SNAP.*.rules")
}

// Tests for Env, Tag and Directory

func (s *cfgSuite) TestEnv(c *C) {
	c.Assert(udev.Env(s.appInfo), DeepEquals, map[string]string{"SNAPPY_APP": "snap.SNAP.APP"})
}

func (s *cfgSuite) TestTag(c *C) {
	c.Assert(udev.Tag, Equals, "snappy-assign")
}

func (s *cfgSuite) TestDirectory(c *C) {
	c.Assert(udev.Directory(), Equals, "/etc/udev/rules.d")
}
