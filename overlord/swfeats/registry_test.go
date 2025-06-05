// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package swfeats_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/swfeats"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type swfeatsSuite struct {
	testutil.BaseTest
	ChangeReg *swfeats.ChangeKindRegistry
	EnsureReg *swfeats.EnsureRegistry
}

var _ = Suite(&swfeatsSuite{})

func (s *swfeatsSuite) SetupSuite(c *C) {
}

func (s *swfeatsSuite) SetUpTest(c *C) {
	s.ChangeReg = swfeats.NewChangeKindRegistry()
	s.EnsureReg = swfeats.AddRegistry()
}

func (s *swfeatsSuite) TestAddChange(c *C) {
	changeKind := s.ChangeReg.Add("my-change")
	c.Assert(changeKind, Equals, "my-change")
}

func (s *swfeatsSuite) TestKnownChangeKinds(c *C) {
	my_change1 := s.ChangeReg.Add("my-change1")
	c.Assert(my_change1, Equals, "my-change1")

	// Add the same change again to check that it isn't added
	// more than once
	my_change1 = s.ChangeReg.Add("my-change1")
	c.Assert(my_change1, Equals, "my-change1")
	my_change2 := s.ChangeReg.Add("my-change2")
	c.Assert(my_change2, Equals, "my-change2")
	changeKinds := s.ChangeReg.KnownChangeKinds()
	c.Assert(changeKinds, HasLen, 2)
	c.Assert(changeKinds, testutil.Contains, "my-change1")
	c.Assert(changeKinds, testutil.Contains, "my-change2")
}

func (s *swfeatsSuite) TestNewChangeTemplateKnown(c *C) {
	changeKind := s.ChangeReg.Add("my-change-%s")
	changeKind2 := s.ChangeReg.Add("my-change-%s")
	c.Assert(changeKind, Equals, changeKind2)
	kinds := s.ChangeReg.KnownChangeKinds()
	// Without possible values added, a templated change will generate
	// the template
	c.Assert(kinds, HasLen, 1)
	c.Assert(kinds, testutil.Contains, "my-change-%s")

	s.ChangeReg.AddVariants(changeKind, []string{"1", "2", "3"})
	kinds = s.ChangeReg.KnownChangeKinds()
	c.Assert(kinds, HasLen, 3)
	c.Assert(kinds, testutil.Contains, "my-change-1")
	c.Assert(kinds, testutil.Contains, "my-change-2")
	c.Assert(kinds, testutil.Contains, "my-change-3")
}

func (s *swfeatsSuite) TestAddEnsure(c *C) {
	c.Assert(s.EnsureReg.KnownEnsures(), HasLen, 0)
	s.EnsureReg.Add("MyManager", "myFunction")
	knownEnsures := s.EnsureReg.KnownEnsures()
	c.Assert(knownEnsures, HasLen, 1)
	c.Assert(knownEnsures, testutil.Contains, swfeats.EnsureEntry{Manager: "MyManager", Function: "myFunction"})
}

func (s *swfeatsSuite) TestDuplicateAdd(c *C) {
	s.EnsureReg.Add("MyManager", "myFunction1")
	s.EnsureReg.Add("MyManager", "myFunction1")
	s.EnsureReg.Add("MyManager", "myFunction2")
	s.EnsureReg.Add("MyManager", "myFunction2")
	knownEnsures := s.EnsureReg.KnownEnsures()
	c.Assert(knownEnsures, HasLen, 2)
	c.Assert(knownEnsures, testutil.Contains, swfeats.EnsureEntry{Manager: "MyManager", Function: "myFunction1"})
	c.Assert(knownEnsures, testutil.Contains, swfeats.EnsureEntry{Manager: "MyManager", Function: "myFunction2"})
}
