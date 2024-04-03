// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package x11_test

import (
	"os"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/x11"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type xauthTestSuite struct{}

var _ = Suite(&xauthTestSuite{})

func (s *xauthTestSuite) TestXauthFileNotAvailable(c *C) {
	err := x11.ValidateXauthorityFile("/does/not/exist")
	c.Assert(err, NotNil)
}

func (s *xauthTestSuite) TestXauthFileExistsButIsEmpty(c *C) {
	xauthPath, err := x11.MockXauthority(0)
	c.Assert(err, IsNil)
	defer os.Remove(xauthPath)

	err = x11.ValidateXauthorityFile(xauthPath)
	c.Assert(err, ErrorMatches, "Xauthority file is invalid")
}

func (s *xauthTestSuite) TestXauthFileExistsButHasInvalidContent(c *C) {
	f, err := os.CreateTemp("", "xauth")
	c.Assert(err, IsNil)
	defer os.Remove(f.Name())

	data := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99}
	n, err := f.Write(data)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, len(data))

	err = x11.ValidateXauthorityFile(f.Name())
	c.Assert(err, ErrorMatches, "unexpected EOF")
}

func (s *xauthTestSuite) TestValidXauthFile(c *C) {
	for _, n := range []int{1, 2, 4} {
		path, err := x11.MockXauthority(n)
		c.Assert(err, IsNil)
		err = x11.ValidateXauthorityFile(path)
		c.Assert(err, IsNil)
	}
}
