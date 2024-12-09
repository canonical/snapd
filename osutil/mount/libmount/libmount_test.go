// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package libmount_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil/mount/libmount"
)

func Test(t *testing.T) { TestingT(t) }

type suite struct{}

var _ = Suite(&suite{})

func (s *suite) TestValidateMountOptions(c *C) {
	c.Check(libmount.ValidateMountOptions("ro", "rw"), ErrorMatches, "option rw conflicts with ro")
	c.Check(libmount.ValidateMountOptions("potato"), ErrorMatches, "option potato is unknown")
	c.Check(libmount.ValidateMountOptions("rw"), IsNil)
}
