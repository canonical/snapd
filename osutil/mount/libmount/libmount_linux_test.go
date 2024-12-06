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
	c.Check(libmount.ValidateMountOptions("rw"), IsNil)
	c.Check(libmount.ValidateMountOptions("ro"), IsNil)
	c.Check(libmount.ValidateMountOptions("ro", "rw"), ErrorMatches, "option rw conflicts with ro")
	c.Check(libmount.ValidateMountOptions("rw", "ro"), ErrorMatches, "option ro conflicts with rw")

	c.Check(libmount.ValidateMountOptions("suid"), IsNil)
	c.Check(libmount.ValidateMountOptions("nosuid"), IsNil)
	c.Check(libmount.ValidateMountOptions("suid", "nosuid"), ErrorMatches, "option nosuid conflicts with suid")
	c.Check(libmount.ValidateMountOptions("nosuid", "suid"), ErrorMatches, "option suid conflicts with nosuid")

	c.Check(libmount.ValidateMountOptions("dev"), IsNil)
	c.Check(libmount.ValidateMountOptions("nodev"), IsNil)
	c.Check(libmount.ValidateMountOptions("dev", "nodev"), ErrorMatches, "option nodev conflicts with dev")
	c.Check(libmount.ValidateMountOptions("nodev", "dev"), ErrorMatches, "option dev conflicts with nodev")

	c.Check(libmount.ValidateMountOptions("exec"), IsNil)
	c.Check(libmount.ValidateMountOptions("noexec"), IsNil)
	c.Check(libmount.ValidateMountOptions("exec", "noexec"), ErrorMatches, "option noexec conflicts with exec")
	c.Check(libmount.ValidateMountOptions("noexec", "exec"), ErrorMatches, "option exec conflicts with noexec")

	c.Check(libmount.ValidateMountOptions("sync"), IsNil)
	c.Check(libmount.ValidateMountOptions("async"), IsNil)
	c.Check(libmount.ValidateMountOptions("sync", "async"), ErrorMatches, "option async conflicts with sync")
	c.Check(libmount.ValidateMountOptions("async", "sync"), ErrorMatches, "option sync conflicts with async")

	c.Check(libmount.ValidateMountOptions("remount"), IsNil)

	c.Check(libmount.ValidateMountOptions("mand"), IsNil)
	c.Check(libmount.ValidateMountOptions("nomand"), IsNil)
	c.Check(libmount.ValidateMountOptions("mand", "nomand"), ErrorMatches, "option nomand conflicts with mand")
	c.Check(libmount.ValidateMountOptions("nomand", "mand"), ErrorMatches, "option mand conflicts with nomand")

	c.Check(libmount.ValidateMountOptions("dirsync"), IsNil)

	c.Check(libmount.ValidateMountOptions("symfollow"), IsNil)
	c.Check(libmount.ValidateMountOptions("nosymfollow"), IsNil)
	c.Check(libmount.ValidateMountOptions("symfollow", "nosymfollow"), ErrorMatches, "option nosymfollow conflicts with symfollow")
	c.Check(libmount.ValidateMountOptions("nosymfollow", "symfollow"), ErrorMatches, "option symfollow conflicts with nosymfollow")

	c.Check(libmount.ValidateMountOptions("atime"), IsNil)
	c.Check(libmount.ValidateMountOptions("noatime"), IsNil)
	c.Check(libmount.ValidateMountOptions("atime", "noatime"), ErrorMatches, "option noatime conflicts with atime")
	c.Check(libmount.ValidateMountOptions("noatime", "atime"), ErrorMatches, "option atime conflicts with noatime")

	c.Check(libmount.ValidateMountOptions("diratime"), IsNil)
	c.Check(libmount.ValidateMountOptions("nodiratime"), IsNil)
	c.Check(libmount.ValidateMountOptions("diratime", "nodiratime"), ErrorMatches, "option nodiratime conflicts with diratime")
	c.Check(libmount.ValidateMountOptions("nodiratime", "diratime"), ErrorMatches, "option diratime conflicts with nodiratime")

	c.Check(libmount.ValidateMountOptions("bind"), IsNil)
	c.Check(libmount.ValidateMountOptions("B"), IsNil)

	c.Check(libmount.ValidateMountOptions("move"), IsNil)
	c.Check(libmount.ValidateMountOptions("M"), IsNil)

	c.Check(libmount.ValidateMountOptions("rbind"), IsNil)
	c.Check(libmount.ValidateMountOptions("R"), IsNil)

	// Silent and verbose are two flags that can be passed together.
	c.Check(libmount.ValidateMountOptions("verbose"), IsNil)
	c.Check(libmount.ValidateMountOptions("silent"), IsNil)
	c.Check(libmount.ValidateMountOptions("loud"), IsNil)

	c.Check(libmount.ValidateMountOptions("acl"), IsNil)
	c.Check(libmount.ValidateMountOptions("noacl"), IsNil)
	c.Check(libmount.ValidateMountOptions("acl", "noacl"), ErrorMatches, "option noacl conflicts with acl")
	c.Check(libmount.ValidateMountOptions("noacl", "acl"), ErrorMatches, "option acl conflicts with noacl")

	c.Check(libmount.ValidateMountOptions("unbindable"), IsNil)
	c.Check(libmount.ValidateMountOptions("make-unbindable"), IsNil)
	c.Check(libmount.ValidateMountOptions("runbindable"), IsNil)
	c.Check(libmount.ValidateMountOptions("make-runbindable"), IsNil)

	c.Check(libmount.ValidateMountOptions("rprivate"), IsNil)
	c.Check(libmount.ValidateMountOptions("make-rprivate"), IsNil)
	c.Check(libmount.ValidateMountOptions("rprivate"), IsNil)
	c.Check(libmount.ValidateMountOptions("make-rprivate"), IsNil)

	c.Check(libmount.ValidateMountOptions("rslave"), IsNil)
	c.Check(libmount.ValidateMountOptions("make-rslave"), IsNil)
	c.Check(libmount.ValidateMountOptions("rslave"), IsNil)
	c.Check(libmount.ValidateMountOptions("make-rslave"), IsNil)

	c.Check(libmount.ValidateMountOptions("rshared"), IsNil)
	c.Check(libmount.ValidateMountOptions("make-rshared"), IsNil)
	c.Check(libmount.ValidateMountOptions("rshared"), IsNil)
	c.Check(libmount.ValidateMountOptions("make-rshared"), IsNil)

	c.Check(libmount.ValidateMountOptions("relatime"), IsNil)
	c.Check(libmount.ValidateMountOptions("norelatime"), IsNil)
	c.Check(libmount.ValidateMountOptions("relatime", "norelatime"), ErrorMatches, "option norelatime conflicts with relatime")
	c.Check(libmount.ValidateMountOptions("norelatime", "relatime"), ErrorMatches, "option relatime conflicts with norelatime")

	c.Check(libmount.ValidateMountOptions("iversion"), IsNil)
	c.Check(libmount.ValidateMountOptions("noiversion"), IsNil)
	c.Check(libmount.ValidateMountOptions("iversion", "noiversion"), ErrorMatches, "option noiversion conflicts with iversion")
	c.Check(libmount.ValidateMountOptions("noiversion", "iversion"), ErrorMatches, "option iversion conflicts with noiversion")

	c.Check(libmount.ValidateMountOptions("strictatime"), IsNil)
	c.Check(libmount.ValidateMountOptions("nostrictatime"), IsNil)
	c.Check(libmount.ValidateMountOptions("strictatime", "nostrictatime"), ErrorMatches, "option nostrictatime conflicts with strictatime")
	c.Check(libmount.ValidateMountOptions("nostrictatime", "strictatime"), ErrorMatches, "option strictatime conflicts with nostrictatime")

	c.Check(libmount.ValidateMountOptions("lazytime"), IsNil)
	c.Check(libmount.ValidateMountOptions("nolazytime"), IsNil)
	c.Check(libmount.ValidateMountOptions("lazytime", "nolazytime"), ErrorMatches, "option nolazytime conflicts with lazytime")
	c.Check(libmount.ValidateMountOptions("nolazytime", "lazytime"), ErrorMatches, "option lazytime conflicts with nolazytime")

	c.Check(libmount.ValidateMountOptions("user"), IsNil)
	c.Check(libmount.ValidateMountOptions("nouser"), IsNil)
	c.Check(libmount.ValidateMountOptions("user", "nouser"), ErrorMatches, "option nouser conflicts with user")
	c.Check(libmount.ValidateMountOptions("nouser", "user"), ErrorMatches, "option user conflicts with nouser")

	c.Check(libmount.ValidateMountOptions("potato"), ErrorMatches, "option potato is unknown")
}
