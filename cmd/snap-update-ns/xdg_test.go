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

package main_test

import (
	"bytes"
	"strings"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/osutil"
)

type xdgSuite struct{}

var _ = Suite(&xdgSuite{})

func (s *xdgSuite) TestXdgRuntimeDir(c *C) {
	c.Check(update.XdgRuntimeDir(1234), Equals, "/run/user/1234")
}

func (s *xdgSuite) TestExpandPrefixVariable(c *C) {
	c.Check(update.ExpandPrefixVariable("$FOO", "$FOO", "/foo"), Equals, "/foo")
	c.Check(update.ExpandPrefixVariable("$FOO/", "$FOO", "/foo"), Equals, "/foo/")
	c.Check(update.ExpandPrefixVariable("$FOO/bar", "$FOO", "/foo"), Equals, "/foo/bar")
	c.Check(update.ExpandPrefixVariable("$FOObar", "$FOO", "/foo"), Equals, "$FOObar")
}

func (s *xdgSuite) TestExpandXdgRuntimeDir(c *C) {
	input := "$XDG_RUNTIME_DIR/doc/by-app/snap.foo $XDG_RUNTIME_DIR/doc none bind,rw 0 0\n"
	output := "/run/user/1234/doc/by-app/snap.foo /run/user/1234/doc none bind,rw 0 0\n"
	profile, err := osutil.ReadMountProfile(strings.NewReader(input))
	c.Assert(err, IsNil)
	update.ExpandXdgRuntimeDir(profile, 1234)
	builder := &bytes.Buffer{}
	profile.WriteTo(builder)
	c.Check(builder.String(), Equals, output)
}
