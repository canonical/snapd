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
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"

	sysd "github.com/snapcore/snapd/systemd"
)

type backendSuite struct {
	ifacetest.BackendSuite
	systemctlCmd *testutil.MockCmd
}

var _ = Suite(&backendSuite{})

var testedConfinementOpts = []interfaces.ConfinementOptions{
	{},
	{DevMode: true},
	{JailMode: true},
	{Classic: true},
}

func (s *backendSuite) SetUpTest(c *C) {
	s.BackendSuite.SetUpTest(c)
	s.Backend = &systemd.Backend{}
	s.systemctlCmd = testutil.MockCommand(c, "systemctl", "echo ActiveState=inactive")
}

func (s *backendSuite) TearDownTest(c *C) {
	s.systemctlCmd.Restore()
	s.BackendSuite.TearDownTest(c)
}

func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, interfaces.SecuritySystemd)
}

func (s *backendSuite) TestUnmarshalRawSnippetMap(c *C) {
	rawSnippetMap := map[string][][]byte{
		"security-tag": {
			[]byte(`{"services": {"foo.service": {"exec-start": "/bin/true"}}}`),
			[]byte(`{"services": {"bar.service": {"exec-start": "/bin/false"}}}`),
		},
	}
	richSnippetMap, err := systemd.UnmarshalRawSnippetMap(rawSnippetMap)
	c.Assert(err, IsNil)
	c.Assert(richSnippetMap, DeepEquals, map[string][]*systemd.Snippet{
		"security-tag": {
			{
				Services: map[string]systemd.Service{
					"foo.service": {ExecStart: "/bin/true"},
				},
			},
			{
				Services: map[string]systemd.Service{
					"bar.service": {ExecStart: "/bin/false"},
				},
			},
		},
	})
}

func (s *backendSuite) TestMergeSnippetMapOK(c *C) {
	snippetMap := map[string][]*systemd.Snippet{
		"security-tag": {
			{
				Services: map[string]systemd.Service{
					"foo.service": {ExecStart: "/bin/true"},
				},
			},
		},
		"another-tag": {
			{
				Services: map[string]systemd.Service{
					"bar.service": {ExecStart: "/bin/false"},
				},
			},
		},
	}
	snippet, err := systemd.MergeSnippetMap(snippetMap)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, &systemd.Snippet{
		Services: map[string]systemd.Service{
			"foo.service": {ExecStart: "/bin/true"},
			"bar.service": {ExecStart: "/bin/false"},
		},
	})
}

func (s *backendSuite) TestMergeSnippetMapClashing(c *C) {
	snippetMap := map[string][]*systemd.Snippet{
		"security-tag": {
			{
				Services: map[string]systemd.Service{
					"foo.service": {ExecStart: "/bin/true"},
				},
			},
		},
		"another-tag": {
			{
				Services: map[string]systemd.Service{
					"foo.service": {ExecStart: "/bin/evil"},
				},
			},
		},
	}
	snippet, err := systemd.MergeSnippetMap(snippetMap)
	c.Assert(err, ErrorMatches, `interface require conflicting system needs`)
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
		"foo.service": {
			Content: []byte("[Service]\nExecStart=/bin/true\n\n[Install]\nWantedBy=multi-user.target\n"),
			Mode:    0644,
		},
	})
}

func (s *backendSuite) TestInstallingSnapWritesStartsServices(c *C) {
	prevctlCmd := sysd.SystemctlCmd
	var sysdLog [][]string
	sysd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		if cmd[0] == "show" {
			return []byte("ActiveState=inactive\n"), nil
		}
		return []byte{}, nil
	}
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte(`{"services": {"snap.samba.interface.foo.service": {"exec-start": "/bin/true"}}}`), nil
	}
	s.InstallSnap(c, interfaces.ConfinementOptions{}, ifacetest.SambaYamlV1, 1)
	service := filepath.Join(dirs.SnapServicesDir, "snap.samba.interface.foo.service")
	// the service file was created
	_, err := os.Stat(service)
	c.Check(err, IsNil)
	// the service was also started (whee)
	c.Check(sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--root", dirs.GlobalRootDir, "enable", "snap.samba.interface.foo.service"},
		{"stop", "snap.samba.interface.foo.service"},
		{"show", "--property=ActiveState", "snap.samba.interface.foo.service"},
		{"start", "snap.samba.interface.foo.service"},
	})
	sysd.SystemctlCmd = prevctlCmd
}

func (s *backendSuite) TestRemovingSnapRemovesAndStopsServices(c *C) {
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte(`{"services": {"snap.samba.interface.foo.service": {"exec-start": "/bin/true"}}}`), nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 1)
		s.systemctlCmd.ForgetCalls()
		s.RemoveSnap(c, snapInfo)
		service := filepath.Join(dirs.SnapServicesDir, "snap.samba.interface.foo.service")
		// the service file was removed
		_, err := os.Stat(service)
		c.Check(os.IsNotExist(err), Equals, true)
		// the service was stopped
		c.Check(s.systemctlCmd.Calls(), DeepEquals, [][]string{
			{"systemctl", "--root", dirs.GlobalRootDir, "disable", "snap.samba.interface.foo.service"},
			{"systemctl", "stop", "snap.samba.interface.foo.service"},
			{"systemctl", "show", "--property=ActiveState", "snap.samba.interface.foo.service"},
			{"systemctl", "daemon-reload"},
		})
	}
}

func (s *backendSuite) TestSettingUpSecurityWithFewerServices(c *C) {
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte(`{"services": {"snap.samba.interface.foo.service": {"exec-start": "/bin/true"}, "snap.samba.interface.bar.service": {"exec-start": "/bin/false"}}}`), nil
	}
	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, ifacetest.SambaYamlV1, 1)
	s.systemctlCmd.ForgetCalls()
	serviceFoo := filepath.Join(dirs.SnapServicesDir, "snap.samba.interface.foo.service")
	serviceBar := filepath.Join(dirs.SnapServicesDir, "snap.samba.interface.bar.service")
	// the services were created
	_, err := os.Stat(serviceFoo)
	c.Check(err, IsNil)
	_, err = os.Stat(serviceBar)
	c.Check(err, IsNil)

	// Change what the interface returns to simulate some useful change
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte(`{"services": {"snap.samba.interface.foo.service": {"exec-start": "/bin/true"}}}`), nil
	}
	// Update over to the same snap to regenerate security
	s.UpdateSnap(c, snapInfo, interfaces.ConfinementOptions{}, ifacetest.SambaYamlV1, 0)
	// The bar service should have been stopped
	c.Check(s.systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "--root", dirs.GlobalRootDir, "disable", "snap.samba.interface.bar.service"},
		{"systemctl", "stop", "snap.samba.interface.bar.service"},
		{"systemctl", "show", "--property=ActiveState", "snap.samba.interface.bar.service"},
		{"systemctl", "daemon-reload"},
	})
}
