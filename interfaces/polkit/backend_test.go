// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package polkit_test

import (
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/polkit"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type backendSuite struct {
	ifacetest.BackendSuite
}

var _ = Suite(&backendSuite{})

var testedConfinementOpts = []interfaces.ConfinementOptions{
	{},
	{DevMode: true},
	{JailMode: true},
	{Classic: true},
}

func (s *backendSuite) SetUpTest(c *C) {
	s.Backend = &polkit.Backend{}
	s.BackendSuite.SetUpTest(c)
	c.Assert(s.Repo.AddBackend(s.Backend), IsNil)
}

func (s *backendSuite) TearDownTest(c *C) {
	s.BackendSuite.TearDownTest(c)
}

// Tests for Setup() and Remove()
func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, interfaces.SecurityPolkit)
}

func (s *backendSuite) TestInstallingSnapWritesPolicyFiles(c *C) {
	// NOTE: Hand out a permanent policy so that .policy file is generated.
	s.Iface.PolkitPermanentSlotCallback = func(spec *polkit.Specification, slot *snap.SlotInfo) error {
		return spec.AddPolicy("foo", polkit.Policy("<policyconfig/>"))
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		policy := filepath.Join(dirs.SnapPolkitPolicyDir, "snap.samba.interface.foo.policy")
		// file called "snap.sambda.interface.foo.policy" was created
		c.Check(policy, testutil.FileContains, "<policyconfig/>")
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesPolicyFiles(c *C) {
	// NOTE: Hand out a permanent snippet so that .policy file is generated.
	s.Iface.PolkitPermanentSlotCallback = func(spec *polkit.Specification, slot *snap.SlotInfo) error {
		return spec.AddPolicy("foo", polkit.Policy("<policyconfig/>"))
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		s.RemoveSnap(c, snapInfo)
		policy := filepath.Join(dirs.SnapPolkitPolicyDir, "snap.samba.interface.foo.policy")
		// file called "snap.sambda.interface.foo.policy" was removed
		c.Check(policy, testutil.FileAbsent)
	}
}

func (s *backendSuite) TestNoPolicyFiles(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		policy := filepath.Join(dirs.SnapPolkitPolicyDir, "snap.samba.interface.foo.policy")
		// Without any snippets, there the .conf file is not created.
		c.Check(policy, testutil.FileAbsent)
		s.RemoveSnap(c, snapInfo)
	}
	c.Check(dirs.SnapPolkitPolicyDir, testutil.FileAbsent)
}

func (s *backendSuite) TestUnexpectedPolicyFilesremoved(c *C) {
	err := os.MkdirAll(dirs.SnapPolkitPolicyDir, 0700)
	c.Assert(err, IsNil)
	policyFile := filepath.Join(dirs.SnapPolkitPolicyDir, "snap.samba.interface.something.policy")

	for _, opts := range testedConfinementOpts {
		c.Assert(os.WriteFile(policyFile, []byte("<policyconfig/>"), 0644), IsNil)
		// Installing snap removes unexpected policy files
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		c.Check(policyFile, testutil.FileAbsent)

		c.Assert(os.WriteFile(policyFile, []byte("<policyconfig/>"), 0644), IsNil)
		// Removing snap also removes unexpected policy files
		s.RemoveSnap(c, snapInfo)
		c.Check(policyFile, testutil.FileAbsent)
	}
}

func (s *backendSuite) TestSandboxFeatures(c *C) {
	c.Assert(s.Backend.SandboxFeatures(), HasLen, 0)
}
