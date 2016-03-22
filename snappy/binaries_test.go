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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
)

type binariesTestSuite struct{}

var _ = Suite(&binariesTestSuite{})

func (s *SnapTestSuite) TestGenerateBinaryName(c *C) {
	c.Check(generateBinaryName(&snapYaml{Name: "foo"}, &AppYaml{Name: "bar"}), Equals, filepath.Join(dirs.SnapBinariesDir, "foo.bar"))
	c.Check(generateBinaryName(&snapYaml{Name: "foo"}, &AppYaml{Name: "foo"}), Equals, filepath.Join(dirs.SnapBinariesDir, "foo"))
}
