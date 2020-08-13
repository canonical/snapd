// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package boot_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
)

type assetsSuite struct {
	baseBootenvSuite
}

var _ = Suite(&assetsSuite{})

func (s *assetsSuite) TestInstallObserverNew(c *C) {
	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model)
	c.Assert(err, IsNil)
	c.Assert(obs, NotNil)

	// but nil for non UC20
	nonUC20Model := makeMockModel()
	nonUC20obs, err := boot.TrustedAssetsInstallObserverForModel(nonUC20Model)
	c.Assert(err, Equals, boot.ErrObserverNotApplicable)
	c.Assert(nonUC20obs, IsNil)
}

func (s *assetsSuite) TestUpdateObserverNew(c *C) {
	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsUpdateObserverForModel(uc20Model)
	c.Assert(err, IsNil)
	c.Assert(obs, NotNil)

	// but nil for non UC20
	nonUC20Model := makeMockModel()
	nonUC20obs, err := boot.TrustedAssetsUpdateObserverForModel(nonUC20Model)
	c.Assert(err, Equals, boot.ErrObserverNotApplicable)
	c.Assert(nonUC20obs, IsNil)
}
