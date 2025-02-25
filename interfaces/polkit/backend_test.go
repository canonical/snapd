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
	"github.com/snapcore/snapd/snap/snaptest"
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

func (s *backendSuite) TestInstallingSnapWritesRuleFiles(c *C) {
	// NOTE: Hand out a permanent rule so that .rules file is generated.
	s.Iface.PolkitPermanentSlotCallback = func(spec *polkit.Specification, slot *snap.SlotInfo) error {
		return spec.AddRule("foo", polkit.Rule("rule content"))
	}
	c.Assert(os.MkdirAll(dirs.SnapPolkitRuleDir, 0755), IsNil)

	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		rule := filepath.Join(dirs.SnapPolkitRuleDir, "70-snap.samba.foo.rules")
		// file called "70-snap.samba.foo.rules" was created
		c.Check(rule, testutil.FileContains, "rule content")
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestInstallingSnapWritesRuleFilesNoRuleDirectory(c *C) {
	s.Iface.PolkitPermanentSlotCallback = func(spec *polkit.Specification, slot *snap.SlotInfo) error {
		return spec.AddRule("foo", polkit.Rule("rule content"))
	}
	c.Assert(os.RemoveAll(dirs.SnapPolkitRuleDir), IsNil)

	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlV1, &snap.SideInfo{
		Revision: snap.R(0),
	})
	appSet, err := interfaces.NewSnapAppSet(snapInfo, nil)
	c.Assert(err, IsNil)
	err = s.Repo.AddAppSet(appSet)
	c.Assert(err, IsNil)
	for _, opts := range testedConfinementOpts {
		err = s.Backend.Setup(appSet, opts, s.Repo, nil)
		c.Assert(err, ErrorMatches, `cannot synchronize polkit rule files for snap "samba":.*: no such file or directory`)
	}
}

func (s *backendSuite) TestInstallingSnapWritesRuleFilesBadNameSuffix(c *C) {
	s.Iface.PolkitPermanentSlotCallback = func(spec *polkit.Specification, slot *snap.SlotInfo) error {
		return spec.AddRule("--", polkit.Rule("rule content"))
	}
	c.Assert(os.RemoveAll(dirs.SnapPolkitRuleDir), IsNil)

	snapInfo := snaptest.MockInfo(c, ifacetest.SambaYamlV1, &snap.SideInfo{
		Revision: snap.R(0),
	})
	appSet, err := interfaces.NewSnapAppSet(snapInfo, nil)
	c.Assert(err, IsNil)
	err = s.Repo.AddAppSet(appSet)
	c.Assert(err, IsNil)
	for _, opts := range testedConfinementOpts {
		err = s.Backend.Setup(appSet, opts, s.Repo, nil)
		c.Assert(err, ErrorMatches, `cannot obtain polkit specification for snap "samba": "--" does not match ".*"`)
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

func (s *backendSuite) TestRemovingSnapRemovesRuleFiles(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.PolkitPermanentSlotCallback = func(spec *polkit.Specification, slot *snap.SlotInfo) error {
		return spec.AddRule("foo", polkit.Rule("rule content"))
	}
	c.Assert(os.MkdirAll(dirs.SnapPolkitRuleDir, 0755), IsNil)

	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		s.RemoveSnap(c, snapInfo)
		rule := filepath.Join(dirs.SnapPolkitRuleDir, "70-snap.samba.foo.rules")
		// file called "70-snap.samba.foo.rules" was removed
		c.Check(rule, testutil.FileAbsent)
	}
}

func (s *backendSuite) TestNoPolicyFiles(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		policy := filepath.Join(dirs.SnapPolkitPolicyDir, "snap.samba.interface.foo.policy")
		// Without any snippets, there the .policy file is not created.
		c.Check(policy, testutil.FileAbsent)
		s.RemoveSnap(c, snapInfo)
	}
	c.Check(dirs.SnapPolkitPolicyDir, testutil.FileAbsent)
}

func (s *backendSuite) TestNoRuleFiles(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		rule := filepath.Join(dirs.SnapPolkitRuleDir, "70-snap.samba.foo.rules")
		// Without any snippets, there the .rules file is not created.
		c.Check(rule, testutil.FileAbsent)
		s.RemoveSnap(c, snapInfo)
	}
	c.Check(dirs.SnapPolkitRuleDir, testutil.FileAbsent)
}

func (s *backendSuite) TestUnexpectedPolicyFilesRemoved(c *C) {
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

func (s *backendSuite) TestUnexpectedRuleFilesRemoved(c *C) {
	err := os.MkdirAll(dirs.SnapPolkitRuleDir, 0700)
	c.Assert(err, IsNil)
	ruleFile := filepath.Join(dirs.SnapPolkitRuleDir, "70-snap.samba.something.rules")

	for _, opts := range testedConfinementOpts {
		c.Assert(os.WriteFile(ruleFile, []byte("rule content"), 0644), IsNil)
		// Installing snap removes unexpected policy files
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		c.Check(ruleFile, testutil.FileAbsent)

		c.Assert(os.WriteFile(ruleFile, []byte("rule content"), 0644), IsNil)
		// Removing snap also removes unexpected policy files
		s.RemoveSnap(c, snapInfo)
		c.Check(ruleFile, testutil.FileAbsent)
	}
}

func (s *backendSuite) TestSandboxFeatures(c *C) {
	c.Assert(s.Backend.SandboxFeatures(), HasLen, 0)
}
