// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"errors"
	"strings"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/osutil"
)

type expandSuite struct{}

var _ = Suite(&expandSuite{})

func (s *expandSuite) TestXdgRuntimeDir(c *C) {
	c.Check(update.XdgRuntimeDir(1234), Equals, "/run/user/1234")
}

func (s *expandSuite) TestExpandPrefixVariable(c *C) {
	value, isExpanded := update.ExpandPrefixVariable("$FOO", "$FOO", "/foo")
	c.Assert(isExpanded, Equals, true)
	c.Check(value, Equals, "/foo")

	value, isExpanded = update.ExpandPrefixVariable("$FOO/", "$FOO", "/foo")
	c.Assert(isExpanded, Equals, true)
	c.Check(value, Equals, "/foo/")

	value, isExpanded = update.ExpandPrefixVariable("$FOO/bar", "$FOO", "/foo")
	c.Assert(isExpanded, Equals, true)
	c.Check(value, Equals, "/foo/bar")

	value, isExpanded = update.ExpandPrefixVariable("$FOObar", "$FOO", "/foo")
	c.Assert(isExpanded, Equals, false)
	c.Check(value, Equals, "$FOObar")

	value, isExpanded = update.ExpandPrefixVariable("$FOO/bar", "$FOO", "")
	c.Assert(isExpanded, Equals, true)
	c.Check(value, Equals, "/bar")
}

func (s *expandSuite) TestExpandXdgRuntimeDir(c *C) {
	input := "$XDG_RUNTIME_DIR/doc/by-app/snap.foo $XDG_RUNTIME_DIR/doc none bind,rw 0 0\n"
	output := "/run/user/1234/doc/by-app/snap.foo /run/user/1234/doc none bind,rw 0 0\n"
	profile, err := osutil.ReadMountProfile(strings.NewReader(input))
	c.Assert(err, IsNil)
	update.ExpandXdgRuntimeDir(profile, 1234)
	builder := &bytes.Buffer{}
	profile.WriteTo(builder)
	c.Check(builder.String(), Equals, output)
}

func (s *expandSuite) TestExpandHomeDirHappy(c *C) {
	input := "none $HOME/.local/share none x-snapd.kind=ensure-dir,x-snapd.must-exist-dir=$HOME 0 0\n" +
		"none $HOME/.local/share none x-snapd.kind=not-ensure-dir,x-snapd.must-exist-dir=$HOME 0 0\n"
	output := "none /home/user/.local/share none x-snapd.kind=ensure-dir,x-snapd.must-exist-dir=/home/user 0 0\n" +
		"none $HOME/.local/share none x-snapd.kind=not-ensure-dir,x-snapd.must-exist-dir=$HOME 0 0\n"
	profile, err := osutil.ReadMountProfile(strings.NewReader(input))
	c.Assert(err, IsNil)
	home := func() (path string, err error) {
		return "/home/user", nil
	}
	c.Assert(update.ExpandHomeDir(profile, home), IsNil)
	builder := &bytes.Buffer{}
	profile.WriteTo(builder)
	c.Check(builder.String(), Equals, output)
}

func (s *expandSuite) TestExpandHomeDirHomeError(c *C) {
	input := "none $HOME/.local/share none x-snapd.kind=ensure-dir,x-snapd.must-exist-dir=$HOME 0 0\n" +
		"none $HOME/.local/share none x-snapd.kind=not-ensure-dir,x-snapd.must-exist-dir=$HOME 0 0\n"
	profile, err := osutil.ReadMountProfile(strings.NewReader(input))
	c.Assert(err, IsNil)
	home := func() (path string, err error) {
		return "/home/user", errors.New("invalid home directory")
	}
	err = update.ExpandHomeDir(profile, home)
	c.Assert(err, ErrorMatches, `cannot expand mount entry \(none \$HOME/.local/share none x-snapd.kind=ensure-dir,x-snapd.must-exist-dir=\$HOME 0 0\): invalid home directory`)
	builder := &bytes.Buffer{}
	profile.WriteTo(builder)
	c.Check(builder.String(), Equals, input)
}
