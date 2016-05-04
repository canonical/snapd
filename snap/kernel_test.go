// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snap_test

import (
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/snap"
)

type KernelYamlTestSuite struct {
}

var _ = Suite(&KernelYamlTestSuite{})

func (s *KernelYamlTestSuite) TestValidateKernelYaml(c *C) {
	err := snap.ValidateKernelYaml([]byte(`version: 4.4-18`))
	c.Assert(err, IsNil)

	err = snap.ValidateKernelYaml([]byte(`version: `))
	c.Assert(err, ErrorMatches, "missing kernel version in kernel.yaml")

	err = snap.ValidateKernelYaml([]byte(``))
	c.Assert(err, ErrorMatches, "missing kernel version in kernel.yaml")
}
