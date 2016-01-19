// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package caps

import (
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/testutil"
)

type RepositorySuite struct {
	t   Type
	cap *Capability
	// Repository pre-populated with s.t
	testRepo *Repository
	// Empty repository
	emptyRepo *Repository
}

var _ = Suite(&RepositorySuite{
	t: &TestType{
		TypeName: "type",
	},
})

func (s *RepositorySuite) SetUpTest(c *C) {
	s.cap = &Capability{
		ID: CapabilityID{
			SnapName: "snap",
			CapName:  "cap",
		},
		Label:    "label",
		TypeName: "type",
	}
	s.testRepo = NewRepository()
	err := s.testRepo.AddType(s.t)
	c.Assert(err, IsNil)
	s.emptyRepo = NewRepository()
}

func (s *RepositorySuite) TestAdd(c *C) {
	c.Assert(s.testRepo.Names(), Not(testutil.Contains), s.cap.String())
	err := s.testRepo.Add(s.cap)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Names(), DeepEquals, []string{"snap.cap"})
	c.Assert(s.testRepo.Names(), testutil.Contains, s.cap.String())
}

func (s *RepositorySuite) TestAddClash(c *C) {
	cap1 := *s.cap
	cap2 := *s.cap
	cap1.Label = "label 1"
	cap2.Label = "label 2"
	err := s.testRepo.Add(&cap1)
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&cap2)
	c.Assert(err, ErrorMatches,
		`cannot add capability "snap.cap": name already exists`)
	c.Assert(s.testRepo.Names(), DeepEquals, []string{"snap.cap"})
	c.Assert(s.testRepo.Names(), testutil.Contains, cap1.String())
}

func (s *RepositorySuite) TestAddInvalidName(c *C) {
	cap1 := &Capability{
		ID: CapabilityID{
			SnapName: "bad-name-",
			CapName:  "good-name",
		},
		Label:    "label",
		TypeName: "type",
	}
	err := s.testRepo.Add(cap1)
	c.Assert(err, ErrorMatches, `"bad-name-" is not a valid snap name`)
	c.Assert(s.testRepo.Names(), HasLen, 0)
	cap2 := &Capability{
		ID: CapabilityID{
			SnapName: "good-name",
			CapName:  "bad-name-",
		},
		Label:    "label",
		TypeName: "type",
	}
	err = s.testRepo.Add(cap2)
	c.Assert(err, ErrorMatches, `"bad-name-" is not a valid snap name`)
	c.Assert(s.testRepo.Names(), HasLen, 0)
}

func (s *RepositorySuite) TestAddType(c *C) {
	t := &TestType{TypeName: "foo"}
	err := s.emptyRepo.AddType(t)
	c.Assert(err, IsNil)
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{"foo"})
	c.Assert(s.emptyRepo.TypeNames(), testutil.Contains, "foo")
}

func (s *RepositorySuite) TestAddTypeClash(c *C) {
	t1 := &TestType{TypeName: "foo"}
	t2 := &TestType{TypeName: "foo"}
	err := s.emptyRepo.AddType(t1)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddType(t2)
	c.Assert(err, ErrorMatches,
		`cannot add type "foo": name already exists`)
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{"foo"})
	c.Assert(s.emptyRepo.TypeNames(), testutil.Contains, "foo")
}

func (s *RepositorySuite) TestAddTypeInvalidName(c *C) {
	t := &TestType{TypeName: "bad-name-"}
	err := s.emptyRepo.AddType(t)
	c.Assert(err, ErrorMatches, `"bad-name-" is not a valid snap name`)
	c.Assert(s.emptyRepo.TypeNames(), HasLen, 0)
}

func (s *RepositorySuite) TestRemoveGood(c *C) {
	err := s.testRepo.Add(s.cap)
	c.Assert(err, IsNil)
	err = s.testRepo.Remove(s.cap.ID)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Names(), HasLen, 0)
}

func (s *RepositorySuite) TestRemoveNoSuchCapability(c *C) {
	err := s.emptyRepo.Remove(s.cap.ID)
	c.Assert(err, ErrorMatches, `can't remove capability "snap.cap", no such capability`)
}

func (s *RepositorySuite) addThreeCapabilities(c *C) {
	// Note added in non-sorted order
	err := s.testRepo.Add(&Capability{
		ID: CapabilityID{
			SnapName: "snap",
			CapName:  "a",
		},
		Label:    "label-a",
		TypeName: "type",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Capability{
		ID: CapabilityID{
			SnapName: "snap",
			CapName:  "c",
		},
		Label:    "label-c",
		TypeName: "type",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Capability{
		ID: CapabilityID{
			SnapName: "snap",
			CapName:  "b",
		},
		Label:    "label-b",
		TypeName: "type",
	})
	c.Assert(err, IsNil)
}

func (s *RepositorySuite) TestNames(c *C) {
	s.addThreeCapabilities(c)
	c.Assert(s.testRepo.Names(), DeepEquals, []string{"snap.a", "snap.b", "snap.c"})
}

func (s *RepositorySuite) TestTypeNames(c *C) {
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{})
	s.emptyRepo.AddType(&TestType{TypeName: "a"})
	s.emptyRepo.AddType(&TestType{TypeName: "b"})
	s.emptyRepo.AddType(&TestType{TypeName: "c"})
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{"a", "b", "c"})
}

func (s *RepositorySuite) TestAll(c *C) {
	s.addThreeCapabilities(c)
	c.Assert(s.testRepo.All(), DeepEquals, []Capability{
		Capability{ID: CapabilityID{"snap", "a"}, Label: "label-a", TypeName: "type"},
		Capability{ID: CapabilityID{"snap", "b"}, Label: "label-b", TypeName: "type"},
		Capability{ID: CapabilityID{"snap", "c"}, Label: "label-c", TypeName: "type"},
	})
}

func (s *RepositorySuite) TestType(c *C) {
	c.Assert(s.emptyRepo.Type(s.t.Name()), IsNil)
	c.Assert(s.testRepo.Type(s.t.Name()), Equals, s.t)
}

func (s *RepositorySuite) TestCapability(c *C) {
	err := s.testRepo.Add(s.cap)
	c.Assert(err, IsNil)
	c.Assert(s.emptyRepo.Capability(s.cap.ID), IsNil)
	c.Assert(s.testRepo.Capability(s.cap.ID), Equals, s.cap)
}

func (s *RepositorySuite) TestHasType(c *C) {
	// hasType works as expected when the object is exactly the one that was
	// added earlier.
	c.Assert(s.emptyRepo.hasType(s.t), Equals, false)
	c.Assert(s.testRepo.hasType(s.t), Equals, true)
	// hasType doesn't do deep equality checks so even though the types are
	// otherwise identical, the test fails.
	c.Assert(s.testRepo.hasType(&TestType{TypeName: s.t.Name()}), Equals, false)
}

func (s *RepositorySuite) TestCaps(c *C) {
	s.addThreeCapabilities(c)
	c.Assert(s.testRepo.Caps(), DeepEquals, map[CapabilityID]*Capability{
		CapabilityID{"snap", "a"}: &Capability{ID: CapabilityID{"snap", "a"}, Label: "label-a", TypeName: "type"},
		CapabilityID{"snap", "b"}: &Capability{ID: CapabilityID{"snap", "b"}, Label: "label-b", TypeName: "type"},
		CapabilityID{"snap", "c"}: &Capability{ID: CapabilityID{"snap", "c"}, Label: "label-c", TypeName: "type"},
	})
}

func (s *RepositorySuite) TestGrantRevoke(c *C) {
	provider := CapabilityID{"provider", "cap"}
	consumer := CapabilityID{"consumer", "slot"}
	r := s.emptyRepo
	c.Assert(r.IsGranted(provider, consumer), Equals, false)
	r.Grant(provider, consumer)
	c.Assert(r.IsGranted(provider, consumer), Equals, true)
	r.Revoke(provider, consumer)
	c.Assert(r.IsGranted(provider, consumer), Equals, false)
}

func (s *RepositorySuite) TestRevokeGrant(c *C) {
	provider := CapabilityID{"provider", "cap"}
	consumer := CapabilityID{"consumer", "slot"}
	r := s.emptyRepo
	c.Assert(r.IsGranted(provider, consumer), Equals, false)
	r.Revoke(provider, consumer)
	c.Assert(r.IsGranted(provider, consumer), Equals, false)
	r.Grant(provider, consumer)
	c.Assert(r.IsGranted(provider, consumer), Equals, true)
}
