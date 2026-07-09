// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package snapstate_test

import (
	"context"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snapdenv"
)

type verifiedPublishersSuite struct {
	snapmgrTestSuite
}

var _ = Suite(&verifiedPublishersSuite{})

func (s *verifiedPublishersSuite) SetUpTest(c *C) {
	s.snapmgrTestSuite.SetUpTest(c)
}

func (s *verifiedPublishersSuite) TestRequireVerifiedPublishersDefault(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", true)

	s.fakeStore.mutateSnapInfo = func(info *snap.Info) error {
		info.Publisher = snap.StoreAccount{
			ID:          "unverified-developer",
			Username:    "unverified-developer",
			DisplayName: "Unverified Developer",
			Validation:  "unproven",
			Verified:    false,
		}
		return nil
	}

	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(ts, NotNil)
}

func (s *verifiedPublishersSuite) TestRequireVerifiedPublishersEnforcedFail(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", true)

	// Enable policy
	tr := config.NewTransaction(s.state)
	err := tr.Set("core", "system.security.required-publisher-validations", "verified")
	c.Assert(err, IsNil)
	tr.Commit()

	s.fakeStore.mutateSnapInfo = func(info *snap.Info) error {
		info.Publisher = snap.StoreAccount{
			ID:          "unverified-developer",
			Username:    "unverified-developer",
			DisplayName: "Unverified Developer",
			Validation:  "unproven",
			Verified:    false,
		}
		return nil
	}

	_, err = snapstate.Install(context.Background(), s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot install snap "some-snap": publisher validation does not match system.security.required-publisher-validations`)
}

func (s *verifiedPublishersSuite) TestRequireVerifiedPublishersEnforcedSuccess(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", true)

	// Enable policy
	tr := config.NewTransaction(s.state)
	err := tr.Set("core", "system.security.required-publisher-validations", "verified")
	c.Assert(err, IsNil)
	tr.Commit()

	s.fakeStore.mutateSnapInfo = func(info *snap.Info) error {
		info.Publisher = snap.StoreAccount{
			ID:          "verified-developer",
			Username:    "verified-developer",
			DisplayName: "Verified Developer",
			Validation:  "verified",
			Verified:    true,
		}
		return nil
	}

	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(ts, NotNil)
}

func (s *verifiedPublishersSuite) TestRequireVerifiedPublishersSeedingBypass(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", true)

	// Mock snapdenv.Preseeding() to true
	restore := snapdenv.MockPreseeding(true)
	defer restore()

	// Enable policy
	tr := config.NewTransaction(s.state)
	err := tr.Set("core", "system.security.required-publisher-validations", "verified")
	c.Assert(err, IsNil)
	tr.Commit()

	s.fakeStore.mutateSnapInfo = func(info *snap.Info) error {
		info.Publisher = snap.StoreAccount{
			ID:          "unverified-developer",
			Username:    "unverified-developer",
			DisplayName: "Unverified Developer",
			Validation:  "unproven",
			Verified:    false,
		}
		return nil
	}

	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(ts, NotNil)
}

func (s *verifiedPublishersSuite) TestAllowStarredPublishersStarredFail(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", true)

	// Enable require-verified-publishers but keep allow-starred-publishers false
	tr := config.NewTransaction(s.state)
	err := tr.Set("core", "system.security.required-publisher-validations", "verified")
	c.Assert(err, IsNil)
	tr.Commit()

	s.fakeStore.mutateSnapInfo = func(info *snap.Info) error {
		info.Publisher = snap.StoreAccount{
			ID:          "starred-developer",
			Username:    "starred-developer",
			DisplayName: "Starred Developer",
			Validation:  "starred",
			Verified:    false,
		}
		return nil
	}

	_, err = snapstate.Install(context.Background(), s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot install snap "some-snap": publisher validation does not match system.security.required-publisher-validations`)
}

func (s *verifiedPublishersSuite) TestAllowStarredPublishersStarredSuccess(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", true)

	// Enable required-publisher-validations with verified and starred
	tr := config.NewTransaction(s.state)
	err := tr.Set("core", "system.security.required-publisher-validations", "verified,starred")
	c.Assert(err, IsNil)
	tr.Commit()

	s.fakeStore.mutateSnapInfo = func(info *snap.Info) error {
		info.Publisher = snap.StoreAccount{
			ID:          "starred-developer",
			Username:    "starred-developer",
			DisplayName: "Starred Developer",
			Validation:  "starred",
			Verified:    false,
		}
		return nil
	}

	ts, err := snapstate.Install(context.Background(), s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(ts, NotNil)
}
