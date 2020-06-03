// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package naming_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/naming"
)

type tagSuite struct{}

var _ = Suite(&tagSuite{})

func (s *tagSuite) TestParseSecurityTag(c *C) {
	// valid snap names, snap instances, app names and hook names are accepted.
	tag, err := naming.ParseSecurityTag("snap.pkg.app")
	c.Assert(err, IsNil)
	c.Check(tag.String(), Equals, "snap.pkg.app")
	c.Check(tag.InstanceName(), Equals, "pkg")
	c.Check(tag.(naming.ParsedAppSecurityTag).AppName(), Equals, "app")

	tag, err = naming.ParseSecurityTag("snap.pkg_key.app")
	c.Assert(err, IsNil)
	c.Check(tag.String(), Equals, "snap.pkg_key.app")
	c.Check(tag.InstanceName(), Equals, "pkg_key")
	c.Check(tag.(naming.ParsedAppSecurityTag).AppName(), Equals, "app")

	tag, err = naming.ParseSecurityTag("snap.pkg.hook.configure")
	c.Assert(err, IsNil)
	c.Check(tag.String(), Equals, "snap.pkg.hook.configure")
	c.Check(tag.InstanceName(), Equals, "pkg")
	c.Check(tag.(naming.ParsedHookSecurityTag).HookName(), Equals, "configure")

	tag, err = naming.ParseSecurityTag("snap.pkg_key.hook.configure")
	c.Assert(err, IsNil)
	c.Check(tag.String(), Equals, "snap.pkg_key.hook.configure")
	c.Check(tag.InstanceName(), Equals, "pkg_key")
	c.Check(tag.(naming.ParsedHookSecurityTag).HookName(), Equals, "configure")

	// invalid format is rejected
	_, err = naming.ParseSecurityTag("snap.pkg.app.surprise")
	c.Check(err, ErrorMatches, "invalid security tag")
	_, err = naming.ParseSecurityTag("snap.pkg_key.app.surprise")
	c.Check(err, ErrorMatches, "invalid security tag")

	// invalid snap and app names are rejected.
	_, err = naming.ParseSecurityTag("snap._.app")
	c.Check(err, ErrorMatches, "invalid security tag")
	_, err = naming.ParseSecurityTag("snap.pkg._")
	c.Check(err, ErrorMatches, "invalid security tag")

	// invalid number of components are rejected.
	_, err = naming.ParseSecurityTag("snap.pkg.hook.surprise.")
	c.Check(err, ErrorMatches, "invalid security tag")
	_, err = naming.ParseSecurityTag("snap.pkg.hook.")
	c.Check(err, ErrorMatches, "invalid security tag")
	tag, err = naming.ParseSecurityTag("snap.pkg.hook")
	c.Assert(err, IsNil) // Perhaps somewhat unexpectedly, this tag is valid.
	c.Check(tag.(naming.ParsedAppSecurityTag).AppName(), Equals, "hook")
	_, err = naming.ParseSecurityTag("snap.pkg.app.surprise")
	c.Check(err, ErrorMatches, "invalid security tag")
	_, err = naming.ParseSecurityTag("snap.pkg.")
	c.Check(err, ErrorMatches, "invalid security tag")
	_, err = naming.ParseSecurityTag("snap.pkg")
	c.Check(err, ErrorMatches, "invalid security tag")
	_, err = naming.ParseSecurityTag("snap.")
	c.Check(err, ErrorMatches, "invalid security tag")
	_, err = naming.ParseSecurityTag("snap")
	c.Check(err, ErrorMatches, "invalid security tag")

	// things that are not snap.* tags
	_, err = naming.ParseSecurityTag("foo.bar.froz")
	c.Check(err, ErrorMatches, "invalid security tag")
}
