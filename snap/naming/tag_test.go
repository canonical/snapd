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
	c.Check(tag.(naming.AppSecurityTag).AppName(), Equals, "app")

	tag, err = naming.ParseSecurityTag("snap.pkg_key.app")
	c.Assert(err, IsNil)
	c.Check(tag.String(), Equals, "snap.pkg_key.app")
	c.Check(tag.InstanceName(), Equals, "pkg_key")
	c.Check(tag.(naming.AppSecurityTag).AppName(), Equals, "app")

	tag, err = naming.ParseSecurityTag("snap.pkg.hook.configure")
	c.Assert(err, IsNil)
	c.Check(tag.String(), Equals, "snap.pkg.hook.configure")
	c.Check(tag.InstanceName(), Equals, "pkg")
	c.Check(tag.(naming.HookSecurityTag).HookName(), Equals, "configure")
	c.Check(tag.(naming.HookSecurityTag).ComponentName(), Equals, "")

	tag, err = naming.ParseSecurityTag("snap.pkg_key.hook.configure")
	c.Assert(err, IsNil)
	c.Check(tag.String(), Equals, "snap.pkg_key.hook.configure")
	c.Check(tag.InstanceName(), Equals, "pkg_key")
	c.Check(tag.(naming.HookSecurityTag).HookName(), Equals, "configure")
	c.Check(tag.(naming.HookSecurityTag).ComponentName(), Equals, "")

	tag, err = naming.ParseSecurityTag("snap.pkg+comp.hook.configure")
	c.Assert(err, IsNil)
	c.Check(tag.String(), Equals, "snap.pkg.hook.configure")
	c.Check(tag.InstanceName(), Equals, "pkg")
	c.Check(tag.(naming.HookSecurityTag).HookName(), Equals, "configure")
	c.Check(tag.(naming.HookSecurityTag).ComponentName(), Equals, "comp")

	tag, err = naming.ParseSecurityTag("snap.pkg_key+comp.hook.configure")
	c.Assert(err, IsNil)
	c.Check(tag.String(), Equals, "snap.pkg_key.hook.configure")
	c.Check(tag.InstanceName(), Equals, "pkg_key")
	c.Check(tag.(naming.HookSecurityTag).HookName(), Equals, "configure")
	c.Check(tag.(naming.HookSecurityTag).ComponentName(), Equals, "comp")

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
	c.Check(tag.(naming.AppSecurityTag).AppName(), Equals, "hook")
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
	_, err = naming.ParseSecurityTag("snap.pkg+.hook.install")
	c.Check(err, ErrorMatches, "invalid security tag")
	_, err = naming.ParseSecurityTag("snap.pkg+comp+comp.hook.install")
	c.Check(err, ErrorMatches, "invalid security tag")

	// things that are not snap.* tags
	_, err = naming.ParseSecurityTag("foo.bar.froz")
	c.Check(err, ErrorMatches, "invalid security tag")
}

func (s *tagSuite) TestParseAppSecurityTag(c *C) {
	// Invalid security tags cannot be parsed.
	tag, err := naming.ParseAppSecurityTag("potato")
	c.Assert(err, ErrorMatches, "invalid security tag")
	c.Assert(tag, IsNil)

	// App security tags can be parsed.
	tag, err = naming.ParseAppSecurityTag("snap.pkg.app")
	c.Assert(err, IsNil)
	c.Check(tag.String(), Equals, "snap.pkg.app")
	c.Check(tag.InstanceName(), Equals, "pkg")
	c.Check(tag.AppName(), Equals, "app")

	// Hook security tags are not app security tags.
	tag, err = naming.ParseAppSecurityTag("snap.pkg.hook.configure")
	c.Assert(err, ErrorMatches, `"snap.pkg.hook.configure" is not an app security tag`)
	c.Assert(tag, IsNil)
}

func (s *tagSuite) TestParseHookSecurityTag(c *C) {
	// Invalid security tags cannot be parsed.
	tag, err := naming.ParseHookSecurityTag("potato")
	c.Assert(err, ErrorMatches, "invalid security tag")
	c.Assert(tag, IsNil)

	// Hook security tags can be parsed.
	tag, err = naming.ParseHookSecurityTag("snap.pkg.hook.configure")
	c.Assert(err, IsNil)
	c.Check(tag.String(), Equals, "snap.pkg.hook.configure")
	c.Check(tag.InstanceName(), Equals, "pkg")
	c.Check(tag.HookName(), Equals, "configure")

	// App security tags are not hook security tags.
	tag, err = naming.ParseHookSecurityTag("snap.pkg.app")
	c.Assert(err, ErrorMatches, `"snap.pkg.app" is not a hook security tag`)
	c.Assert(tag, IsNil)
}
