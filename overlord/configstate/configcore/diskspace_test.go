// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

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

package configcore_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type diskSpaceSuite struct {
	configcoreSuite
}

var _ = Suite(&diskSpaceSuite{})

func (s *diskSpaceSuite) TestConfigureDiskSpaceReservation(c *C) {
	for _, tc := range []struct {
		value any
		err   string
	}{
		{value: ""},
		{value: "0"},
		{value: 0},
		{value: "5242880"},
		{value: "5M"},
		{value: "1G"},
		{value: "0B", err: `invalid suffix "B"`},
		{value: "5MB", err: `invalid suffix "MB"`},
		{value: "5MiB", err: `invalid suffix "MiB"`},
		{value: "-1", err: `size cannot be negative`},
		{value: "bad", err: `no numerical prefix`},
	} {
		err := configcore.Run(classicDev, &mockConf{
			state: s.state,
			changes: map[string]any{
				"disk-reservation.size": tc.value,
			},
		})
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *diskSpaceSuite) TestMigrateDiskSpaceReservationFeatureFlags(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	for _, feature := range []features.SnapdFeature{
		features.CheckDiskSpaceInstall,
		features.CheckDiskSpaceRefresh,
		features.CheckDiskSpaceRemove,
	} {
		tr := config.NewTransaction(s.state)
		snapName, confName := feature.ConfigOption()
		c.Assert(tr.Set(snapName, confName, true), IsNil)

		runTr := configcore.NewRunTransaction(tr, nil)
		c.Assert(configcore.MigrateDiskSpaceReservation(runTr), IsNil)

		var reservation uint64
		c.Assert(tr.Get("core", "disk-reservation.size", &reservation), IsNil)
		c.Check(reservation, Equals, uint64(5*1024*1024))

		c.Assert(tr.Set("core", "disk-reservation.size", nil), IsNil)
		c.Assert(tr.Set(snapName, confName, nil), IsNil)
		tr.Commit()
	}
}

func (s *diskSpaceSuite) TestMigrateDiskSpaceReservationPreservesConfiguredValue(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "experimental.check-disk-space-install", true), IsNil)
	c.Assert(tr.Set("core", "disk-reservation.size", 0), IsNil)

	runTr := configcore.NewRunTransaction(tr, nil)
	c.Assert(configcore.MigrateDiskSpaceReservation(runTr), IsNil)

	var reservation uint64
	c.Assert(tr.Get("core", "disk-reservation.size", &reservation), IsNil)
	c.Check(reservation, Equals, uint64(0))
}

func (s *diskSpaceSuite) TestMigrateDiskSpaceReservationDoesNothingWhenFeaturesDisabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	runTr := configcore.NewRunTransaction(tr, nil)
	c.Assert(configcore.MigrateDiskSpaceReservation(runTr), IsNil)

	var reservation any
	c.Check(config.IsNoOption(tr.Get("core", "disk-reservation.size", &reservation)), Equals, true)
}
