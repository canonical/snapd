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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	snapstatetest "github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/snap"
)

type verifiedPublishersSuite struct {
	snapmgrTestSuite
}

var _ = Suite(&verifiedPublishersSuite{})

func (s *verifiedPublishersSuite) SetUpTest(c *C) {
	s.snapmgrTestSuite.SetUpTest(c)
}

func (s *verifiedPublishersSuite) makeSnapsup(snapName, snapID string, publisher snap.StoreAccount) *snapstate.SnapSetup {
	return &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName,
			SnapID:   snapID,
		},
		Publisher: publisher,
	}
}

// TestRequireVerifiedPublishersDefault verifies that with no policy configured all snaps pass publisher validation.
func (s *verifiedPublishersSuite) TestRequireVerifiedPublishersDefault(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapsup := s.makeSnapsup("some-snap", "some-snap-id", snap.StoreAccount{
		ID:         "unverified-developer",
		Username:   "unverified-developer",
		Validation: "unproven",
	})

	err := snapstate.CheckVerifiedPublisher(s.state, snapsup)
	c.Assert(err, IsNil)
}

// TestRequireVerifiedPublishersEnforcedFail verifies that unverified snaps are rejected when the policy requires verified publishers.
func (s *verifiedPublishersSuite) TestRequireVerifiedPublishersEnforcedFail(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	err := tr.Set("core", "system.security.required-publisher-validations", "verified")
	c.Assert(err, IsNil)
	tr.Commit()

	snapsup := s.makeSnapsup("some-snap", "some-snap-id", snap.StoreAccount{
		ID:         "unverified-developer",
		Username:   "unverified-developer",
		Validation: "unproven",
	})

	err = snapstate.CheckVerifiedPublisher(s.state, snapsup)
	c.Assert(err, ErrorMatches, `cannot install snap "some-snap": publisher validation does not match system.security.required-publisher-validations`)
}

// TestRequireVerifiedPublishersEnforcedSuccess verifies that verified snaps are allowed when the policy requires verified publishers.
func (s *verifiedPublishersSuite) TestRequireVerifiedPublishersEnforcedSuccess(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	err := tr.Set("core", "system.security.required-publisher-validations", "verified")
	c.Assert(err, IsNil)
	tr.Commit()

	snapsup := s.makeSnapsup("some-snap", "some-snap-id", snap.StoreAccount{
		ID:         "verified-developer",
		Username:   "verified-developer",
		Validation: "verified",
	})

	err = snapstate.CheckVerifiedPublisher(s.state, snapsup)
	c.Assert(err, IsNil)
}

// TestRequireVerifiedPublishersPrivateSnapBypass verifies that private snaps are excluded from publisher validation enforcement.
func (s *verifiedPublishersSuite) TestRequireVerifiedPublishersPrivateSnapBypass(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	err := tr.Set("core", "system.security.required-publisher-validations", "verified")
	c.Assert(err, IsNil)
	tr.Commit()

	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "private-snap",
			SnapID:   "private-snap-id",
			Private:  true,
		},
		Publisher: snap.StoreAccount{
			ID:         "unverified-developer",
			Username:   "unverified-developer",
			Validation: "unproven",
		},
	}

	err = snapstate.CheckVerifiedPublisher(s.state, snapsup)
	c.Assert(err, IsNil)
}

// TestAllowStarredPublishersStarredFail verifies that starred-only snaps are rejected when the policy only lists "verified".
func (s *verifiedPublishersSuite) TestAllowStarredPublishersStarredFail(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	err := tr.Set("core", "system.security.required-publisher-validations", "verified")
	c.Assert(err, IsNil)
	tr.Commit()

	snapsup := s.makeSnapsup("some-snap", "some-snap-id", snap.StoreAccount{
		ID:         "starred-developer",
		Username:   "starred-developer",
		Validation: "starred",
	})

	err = snapstate.CheckVerifiedPublisher(s.state, snapsup)
	c.Assert(err, ErrorMatches, `cannot install snap "some-snap": publisher validation does not match system.security.required-publisher-validations`)
}

// TestAllowStarredPublishersStarredSuccess verifies that starred snaps pass when the policy includes "starred".
func (s *verifiedPublishersSuite) TestAllowStarredPublishersStarredSuccess(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	err := tr.Set("core", "system.security.required-publisher-validations", "verified,starred")
	c.Assert(err, IsNil)
	tr.Commit()

	snapsup := s.makeSnapsup("some-snap", "some-snap-id", snap.StoreAccount{
		ID:         "starred-developer",
		Username:   "starred-developer",
		Validation: "starred",
	})

	err = snapstate.CheckVerifiedPublisher(s.state, snapsup)
	c.Assert(err, IsNil)
}

// TestRequireVerifiedPublishersUpdateFail verifies that updating an already-installed snap whose publisher validation has dropped is blocked.
func (s *verifiedPublishersSuite) TestRequireVerifiedPublishersUpdateFail(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// Simulate an already-installed snap
	snapst := snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current: snap.R(1),
	}
	snapstate.Set(s.state, "some-snap", &snapst)

	tr := config.NewTransaction(s.state)
	err := tr.Set("core", "system.security.required-publisher-validations", "verified")
	c.Assert(err, IsNil)
	tr.Commit()

	snapsup := s.makeSnapsup("some-snap", "some-snap-id", snap.StoreAccount{
		ID:         "unverified-developer",
		Username:   "unverified-developer",
		Validation: "unproven",
	})

	err = snapstate.CheckVerifiedPublisher(s.state, snapsup)
	c.Assert(err, ErrorMatches, `cannot update snap "some-snap": publisher validation does not match system.security.required-publisher-validations`)
}
