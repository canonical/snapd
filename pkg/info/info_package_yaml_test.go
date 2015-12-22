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

package info

import (
	"testing"

	"github.com/ubuntu-core/snappy/pkg"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type InfoPackageYamlTestSuite struct {
}

var _ = Suite(&InfoPackageYamlTestSuite{})

var mockYaml = []byte(`name: foo
version: 1.0
type: app
`)

func (s *InfoPackageYamlTestSuite) TestSimple(c *C) {
	info, err := NewFromPackageYaml(mockYaml)
	c.Assert(err, IsNil)
	c.Assert(info.Name(), Equals, "foo")
	c.Assert(info.Version(), Equals, "1.0")
	c.Assert(info.Type(), Equals, pkg.TypeApp)
}

func (s *InfoPackageYamlTestSuite) TestFail(c *C) {
	_, err := NewFromPackageYaml([]byte("random-crap"))
	c.Assert(err, ErrorMatches, "(?m)info failed to parse:.*")
}
