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

package seccomp_test

import (
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/interfaces/seccomp"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/testutil"
)

type cfgSuite struct {
	cfg      interfaces.SecurityBackend
	snapInfo *snap.Info
	appInfo  *snap.AppInfo
}

var _ = Suite(&cfgSuite{cfg: &seccomp.Configurator{}})

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
	c.Assert(s.cfg.SecuritySystem(), Equals, interfaces.SecuritySecComp)
}

// Tests for Configurator.DirStateForInstalledSnap()

func (s *cfgSuite) TestDirStateForInstalledSnap(c *C) {
	restore := seccomp.MockTemplate([]byte("default"))
	defer restore()
	for _, scenario := range []struct {
		developerMode bool
		snippets      map[string][][]byte
		content       map[string]*osutil.FileState
	}{
		// no snippets, no just the default template
		{
			content: map[string]*osutil.FileState{
				"snap.SNAP.APP": {
					Content: []byte("default"),
					Mode:    0644,
				},
			},
		},
		{
			snippets: map[string][][]byte{
				"APP": {[]byte("snippet1"), []byte("snippet2")},
			},
			content: map[string]*osutil.FileState{
				"snap.SNAP.APP": {
					Content: []byte("default\nsnippet1\nsnippet2"),
					Mode:    0644,
				},
			},
		},
		{
			developerMode: true,
			content: map[string]*osutil.FileState{
				"snap.SNAP.APP": {
					Content: []byte("@unrestricted\n"),
					Mode:    0644,
				},
			},
		},
		{
			developerMode: true,
			snippets: map[string][][]byte{
				"APP": {[]byte("snippet1"), []byte("snippet2")},
			},
			content: map[string]*osutil.FileState{
				"snap.SNAP.APP": {
					Content: []byte("@unrestricted\n"),
					Mode:    0644,
				},
			},
		},
	} {
		dir, glob, content, err := s.cfg.DirStateForInstalledSnap(
			s.snapInfo, scenario.developerMode, scenario.snippets)
		c.Assert(err, IsNil)
		c.Check(dir, Equals, seccomp.Directory())
		c.Check(glob, Equals, "snap.SNAP.*")
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
	}
}

func (s *cfgSuite) TestRealDefaultTemplateIsNormallyUsed(c *C) {
	// NOTE: we don't call seccomp.MockTemplate()
	_, _, content, err := s.cfg.DirStateForInstalledSnap(s.snapInfo, false, nil)
	c.Assert(err, IsNil)
	profile := string(content["snap.SNAP.APP"].Content)
	for _, line := range []string{
		// NOTE: a few randomly picked lines from the real profile.  Comments
		// and empty lines are avoided as those can be discarded in the future.
		"deny kexec_load\n",
		"deny create_module\n",
		"symlink\n",
		"socket\n",
		"pwritev\n",
	} {
		c.Assert(profile, testutil.Contains, line)
	}
}

// Tests for Configurator.DirStateForRemovedSnap()

func (s *cfgSuite) TestDirStateForRemovedSnap(c *C) {
	dir, glob := s.cfg.DirStateForRemovedSnap(s.snapInfo)
	c.Check(dir, Equals, seccomp.Directory())
	c.Check(glob, Equals, "snap.SNAP.*")
}

// Tests for ProfileFile and Directory

func (s *cfgSuite) TestProfileFile(c *C) {
	c.Assert(seccomp.ProfileFile(s.appInfo), Equals, "snap.SNAP.APP")
}

func (s *cfgSuite) TestDirectory(c *C) {
	c.Assert(seccomp.Directory(), Equals, "/var/lib/snappy/seccomp/profiles")
}
