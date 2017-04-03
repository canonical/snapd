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
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/x11"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type XauthTestSuite struct {}

var _ = Suite(&XauthTestSuite{})

func (s *XauthTestSuite) TestXauthFileNotAvailable(c *C) {
	n, err := x11.ValidateXauthority("/does/not/exist")
	c.Assert(n, Equals, 0)
	c.Assert(err, Not(IsNil))
}

func (s *XauthTestSuite) TestXauthFileExistsButIsEmpty(c *C) {
	f, err := ioutil.TempFile("", "xauth")
	c.Assert(err, IsNil)
	defer os.Remove(f.Name())

	n, err := x11.ValidateXauthority(f.Name())
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 0)
}

func (s *XauthTestSuite) TestXauthFileExistsButHasInvalidContent(c *C) {
	f, err := ioutil.TempFile("", "xauth")
	c.Assert(err, IsNil)
	defer os.Remove(f.Name())

	data := []byte{0x11,0x22,0x33,0x44,0x55,0x66,0x77,0x88,0x99}
	n, err := f.Write(data)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, len(data))

	n, err = x11.ValidateXauthority(f.Name())
	c.Assert(err, DeepEquals, fmt.Errorf("Could not read enough bytes"))
	c.Assert(n, Equals, 0)
}

func (s *XauthTestSuite) TestValidXauthFile(c *C) {
	path := x11.MockXauthority(1)
	n, err := x11.ValidateXauthority(path)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 1)
}
