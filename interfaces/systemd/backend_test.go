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

package systemd_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/backendtest"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type backendSuite struct {
	backendtest.BackendSuite
	systemctlCmd *testutil.MockCmd
}

var _ = Suite(&backendSuite{})

func (s *backendSuite) SetUpTest(c *C) {
	s.BackendSuite.SetUpTest(c)
	s.Backend = &systemd.Backend{}
	// Prepare a directory for systemd units
	// NOTE: Normally this is a part of the OS snap.
	err := os.MkdirAll(dirs.SnapServicesDir, 0700)
	c.Assert(err, IsNil)
	// Mock away systemd interaction
	s.systemctlCmd = testutil.MockCommand(c, "systemctl", "")
}

func (s *backendSuite) TearDownTest(c *C) {
	s.systemctlCmd.Restore()
	s.BackendSuite.TearDownTest(c)
}

func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, "systemd")
}

func (s *backendSuite) TestUnmarshalRawSnippetMap(c *C) {
	rawSnippetMap := map[string][][]byte{
		"security-tag": [][]byte{
			[]byte(`{"services": {"foo.service": {"exec-start": "/bin/true"}}}`),
			[]byte(`{"services": {"bar.service": {"exec-start": "/bin/false"}}}`),
		},
	}
	richSnippetMap, err := systemd.UnmarshalRawSnippetMap(rawSnippetMap)
	c.Assert(err, IsNil)
	c.Assert(richSnippetMap, DeepEquals, map[string][]*systemd.Snippet{
		"security-tag": []*systemd.Snippet{
			&systemd.Snippet{
				Services: map[string]systemd.Service{
					"foo.service": {ExecStart: "/bin/true"},
				},
			},
			&systemd.Snippet{
				Services: map[string]systemd.Service{
					"bar.service": {ExecStart: "/bin/false"},
				},
			},
		},
	})
}

func (s *backendSuite) TestFlattenSnippetMapOK(c *C) {
	snippetMap := map[string][]*systemd.Snippet{
		"security-tag": []*systemd.Snippet{
			&systemd.Snippet{
				Services: map[string]systemd.Service{
					"foo.service": {ExecStart: "/bin/true"},
				},
			},
		},
		"another-tag": []*systemd.Snippet{
			&systemd.Snippet{
				Services: map[string]systemd.Service{
					"bar.service": {ExecStart: "/bin/false"},
				},
			},
		},
	}
	snippet, err := systemd.FlattenSnippetMap(snippetMap)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, &systemd.Snippet{
		Services: map[string]systemd.Service{
			"foo.service": {ExecStart: "/bin/true"},
			"bar.service": {ExecStart: "/bin/false"},
		},
	})
}

func (s *backendSuite) TestFlattenSnippetMapClashing(c *C) {
	snippetMap := map[string][]*systemd.Snippet{
		"security-tag": []*systemd.Snippet{
			&systemd.Snippet{
				Services: map[string]systemd.Service{
					"foo.service": {ExecStart: "/bin/true"},
				},
			},
		},
		"another-tag": []*systemd.Snippet{
			&systemd.Snippet{
				Services: map[string]systemd.Service{
					"foo.service": {ExecStart: "/bin/evil"},
				},
			},
		},
	}
	snippet, err := systemd.FlattenSnippetMap(snippetMap)
	c.Assert(err, ErrorMatches, `cannot merge two diferent services competing for name "foo.service"`)
	c.Assert(snippet, IsNil)
}

func (s *backendSuite) TestRenderSnippet(c *C) {
	snippet := &systemd.Snippet{
		Services: map[string]systemd.Service{
			"foo.service": {ExecStart: "/bin/true"},
		},
	}
	content, err := systemd.RenderSnippet(snippet)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, map[string]*osutil.FileState{
		"foo.service": &osutil.FileState{
			Content: []byte("[Service]\nExecStart=/bin/true\n"),
			Mode:    0644,
		},
	})
}

func (s *backendSuite) TestInstallingSnapWritesStartsServices(c *C) {
	devMode := false
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte(`{"services": {"snap.samba.-.foo.service": {"exec-start": "/bin/true"}}}`), nil
	}
	s.InstallSnap(c, devMode, backendtest.SambaYamlV1, 1)
	service := filepath.Join(dirs.SnapServicesDir, "snap.samba.-.foo.service")
	// the service file was created
	_, err := os.Stat(service)
	c.Check(err, IsNil)
	// the service was also started (whee)
	c.Check(s.systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "start", "snap.samba.-.foo.service"},
	})
}

func (s *backendSuite) TestRemovingSnapRemovesAndStopsServices(c *C) {
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte(`{"services": {"snap.samba.-.foo.service": {"exec-start": "/bin/true"}}}`), nil
	}
	for _, devMode := range []bool{true, false} {
		snapInfo := s.InstallSnap(c, devMode, backendtest.SambaYamlV1, 1)
		s.systemctlCmd.ForgetCalls()
		s.RemoveSnap(c, snapInfo)
		service := filepath.Join(dirs.SnapServicesDir, "snap.samba.-.foo.service")
		// the service file was removed
		_, err := os.Stat(service)
		c.Check(os.IsNotExist(err), Equals, true)
		// the service was stopped
		c.Check(s.systemctlCmd.Calls(), DeepEquals, [][]string{
			{"systemctl", "stop", "snap.samba.-.foo.service"},
			{"systemctl", "show", "--property=ActiveState", "snap.samba.-.foo.service"},
			{"systemctl", "show", "--property=ActiveState", "snap.samba.-.foo.service"},
			{"systemctl", "show", "--property=ActiveState", "snap.samba.-.foo.service"},
			{"systemctl", "daemon-reload"},
		})
	}
}
