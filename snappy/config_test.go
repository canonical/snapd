// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	. "gopkg.in/check.v1"
)

var mockOsSnap = `
name: ubuntu-core
version: 1.0
type: os
`

func (s *SnapTestSuite) TestConfigOS(c *C) {
	snapYaml, err := makeInstalledMockSnap(mockOsSnap, 11)
	c.Assert(err, IsNil)
	snap, err := NewInstalledSnap(snapYaml)
	c.Assert(err, IsNil)

	var cfg []byte
	inCfg := []byte(`something`)
	coreConfig = func(configuration []byte) ([]byte, error) {
		cfg = configuration
		return cfg, nil
	}
	defer func() { coreConfig = coreConfigImpl }()

	_, err = (&Overlord{}).Configure(snap, inCfg)
	c.Assert(err, IsNil)
	c.Assert(cfg, DeepEquals, inCfg)
}
