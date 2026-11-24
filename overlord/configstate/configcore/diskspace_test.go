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
	"fmt"

	. "gopkg.in/check.v1"

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

func (s *diskSpaceSuite) TestGraduatedDiskSpaceFlagsPreserveReservation(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	for _, feature := range []string{
		"check-disk-space-install",
		"check-disk-space-refresh",
		"check-disk-space-remove",
	} {
		for _, reservation := range []any{nil, 0, "1G"} {
			setupTr := config.NewTransaction(s.state)
			c.Assert(setupTr.Set("core", "disk-reservation.size", reservation), IsNil)
			setupTr.Commit()

			tr := configcore.NewRunTransaction(config.NewTransaction(s.state), nil)
			c.Assert(tr.Set("core", "experimental."+feature, true), IsNil)

			s.state.Unlock()
			err := configcore.Run(classicDev, tr)
			s.state.Lock()
			c.Assert(err, IsNil)
			tr.Commit()

			var enabled bool
			c.Check(config.IsNoOption(tr.Get("core", "experimental."+feature, &enabled)), Equals, true)

			var value any
			err = tr.Get("core", "disk-reservation.size", &value)
			if reservation == nil {
				c.Check(config.IsNoOption(err), Equals, true)
			} else {
				c.Assert(err, IsNil)
				c.Check(fmt.Sprint(value), Equals, fmt.Sprint(reservation))
			}
		}
	}
}
