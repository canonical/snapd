// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package bootstrap_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/bootstrap"
)

func TestBootstrap(t *testing.T) { TestingT(t) }

type bootstrapSuite struct{}

var _ = Suite(&bootstrapSuite{})

// XXX: write a very high level integration like test here that
// mocks the world (sfdisk,lsblk,mkfs,...)? probably silly as
// each part inside bootstrap is tested and we have a spread test

func (s *bootstrapSuite) TestBootstrapRunError(c *C) {
	err := bootstrap.Run("", "", nil)
	c.Assert(err, ErrorMatches, "cannot use empty gadget root directory")

	err = bootstrap.Run("some-dir", "", nil)
	c.Assert(err, ErrorMatches, "cannot use empty device node")
}
