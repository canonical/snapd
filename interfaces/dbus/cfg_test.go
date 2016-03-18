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

package dbus_test

import (
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/interfaces/dbus"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
)

type cfgSuite struct {
	cfg      interfaces.SecurityBackend
	snapInfo *snap.Info
	appInfo  *snap.AppInfo
}

var _ = Suite(&cfgSuite{cfg: &dbus.Configurator{}})

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
	c.Assert(s.cfg.SecuritySystem(), Equals, interfaces.SecurityDBus)
}

// Tests for Configurator.DirStateForInstalledSnap()

func (s *cfgSuite) TestDirStateForInstalledSnap(c *C) {
	restore := dbus.MockXMLEnvelope([]byte("<?xml>"), []byte("</xml>"))
	defer restore()
	for _, scenario := range []struct {
		developerMode bool
		snippets      map[string][][]byte
		content       map[string]*osutil.FileState
	}{
		{}, // no snippets, no content
		{
			snippets: map[string][][]byte{
				"APP": {[]byte("<policy> 1 </policy>"), []byte("<policy> 2 </policy>")},
			},
			content: map[string]*osutil.FileState{
				"snap.SNAP.APP.conf": {
					Content: []byte("<?xml>\n<policy> 1 </policy>\n<policy> 2 </policy>\n</xml>"),
					Mode:    0644,
				},
			},
		},
		{developerMode: true},
		{
			// FIXME: real developer mode
			developerMode: true,
			snippets: map[string][][]byte{
				"APP": {[]byte("<policy> 1 </policy>"), []byte("<policy> 2 </policy>")},
			},
			content: map[string]*osutil.FileState{
				"snap.SNAP.APP.conf": {
					Content: []byte("<?xml>\n<policy> 1 </policy>\n<policy> 2 </policy>\n</xml>"),
					Mode:    0644,
				},
			},
		},
	} {
		dir, glob, content, err := s.cfg.DirStateForInstalledSnap(s.snapInfo, scenario.developerMode, scenario.snippets)
		c.Assert(err, IsNil)
		c.Check(dir, Equals, dbus.Directory())
		c.Check(glob, Equals, "snap.SNAP.*.conf")
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
		// Ensure that the file name will be picked up by dbus.
		for name := range content {
			c.Check(strings.HasSuffix(name, ".conf"), Equals, true)
		}
	}
}

func (s *cfgSuite) TestRealXMLHeaderAndFooterAreNormallyUsed(c *C) {
	// NOTE: we don't call dbus.MockXMLEnvelope()
	_, _, content, err := s.cfg.DirStateForInstalledSnap(
		s.snapInfo, false, map[string][][]byte{"APP": {[]byte("<!-- test -->")}})
	c.Assert(err, IsNil)
	profile := string(content["snap.SNAP.APP.conf"].Content)
	c.Check(profile, Equals, `<!DOCTYPE busconfig PUBLIC\
 "-//freedesktop//DTD D-BUS Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
<!-- test -->
</busconfig>`)
}

// Tests for Configurator.DirStateForRemovedSnap()

func (s *cfgSuite) TestDirStateForRemovedSnap(c *C) {
	dir, glob := s.cfg.DirStateForRemovedSnap(s.snapInfo)
	c.Check(dir, Equals, dbus.Directory())
	c.Check(glob, Equals, "snap.SNAP.*.conf")
}

// Test for Directory

func (s *cfgSuite) TestDirectory(c *C) {
	c.Assert(dbus.Directory(), Equals, "/etc/dbus-1/system.d")
}
