package prompting_test

import (
	"github.com/snapcore/snapd/sandbox/apparmor/prompting"

	. "gopkg.in/check.v1"
)

type permissionSuite struct{}

var _ = Suite(&permissionSuite{})

func (*permissionSuite) TestExactValues(c *C) {
	// The specific values must match sys/apparmor.h
	c.Check(prompting.AA_MAY_EXEC, Equals, prompting.FilePermission(1<<0))
	c.Check(prompting.AA_MAY_WRITE, Equals, prompting.FilePermission(1<<1))
	c.Check(prompting.AA_MAY_READ, Equals, prompting.FilePermission(1<<2))
	c.Check(prompting.AA_MAY_APPEND, Equals, prompting.FilePermission(1<<3))
	c.Check(prompting.AA_MAY_CREATE, Equals, prompting.FilePermission(1<<4))
	c.Check(prompting.AA_MAY_DELETE, Equals, prompting.FilePermission(1<<5))
	c.Check(prompting.AA_MAY_OPEN, Equals, prompting.FilePermission(1<<6))
	c.Check(prompting.AA_MAY_RENAME, Equals, prompting.FilePermission(1<<7))
	c.Check(prompting.AA_MAY_SETATTR, Equals, prompting.FilePermission(1<<8))
	c.Check(prompting.AA_MAY_GETATTR, Equals, prompting.FilePermission(1<<9))
	c.Check(prompting.AA_MAY_SETCRED, Equals, prompting.FilePermission(1<<10))
	c.Check(prompting.AA_MAY_GETCRED, Equals, prompting.FilePermission(1<<11))
	c.Check(prompting.AA_MAY_CHMOD, Equals, prompting.FilePermission(1<<12))
	c.Check(prompting.AA_MAY_CHOWN, Equals, prompting.FilePermission(1<<13))
	c.Check(prompting.AA_MAY_CHGRP, Equals, prompting.FilePermission(1<<14))
	c.Check(prompting.AA_MAY_LOCK, Equals, prompting.FilePermission(0x8000))
	c.Check(prompting.AA_EXEC_MMAP, Equals, prompting.FilePermission(0x10000))
	c.Check(prompting.AA_MAY_LINK, Equals, prompting.FilePermission(0x40000))
	c.Check(prompting.AA_MAY_ONEXEC, Equals, prompting.FilePermission(0x20000000))
	c.Check(prompting.AA_MAY_CHANGE_PROFILE, Equals, prompting.FilePermission(0x40000000))
}

func (*permissionSuite) TestFilePermissionString(c *C) {
	// No permission bits set.
	c.Check(prompting.FilePermission(0).String(), Equals, "none")

	// Specific single permission bit set.
	c.Check(prompting.AA_MAY_EXEC.String(), Equals, "execute")
	c.Check(prompting.AA_MAY_WRITE.String(), Equals, "write")
	c.Check(prompting.AA_MAY_READ.String(), Equals, "read")
	c.Check(prompting.AA_MAY_APPEND.String(), Equals, "append")
	c.Check(prompting.AA_MAY_CREATE.String(), Equals, "create")
	c.Check(prompting.AA_MAY_DELETE.String(), Equals, "delete")
	c.Check(prompting.AA_MAY_OPEN.String(), Equals, "open")
	c.Check(prompting.AA_MAY_RENAME.String(), Equals, "rename")
	c.Check(prompting.AA_MAY_SETATTR.String(), Equals, "set-attr")
	c.Check(prompting.AA_MAY_GETATTR.String(), Equals, "get-attr")
	c.Check(prompting.AA_MAY_SETCRED.String(), Equals, "set-cred")
	c.Check(prompting.AA_MAY_GETCRED.String(), Equals, "get-cred")
	c.Check(prompting.AA_MAY_CHMOD.String(), Equals, "change-mode")
	c.Check(prompting.AA_MAY_CHOWN.String(), Equals, "change-owner")
	c.Check(prompting.AA_MAY_CHGRP.String(), Equals, "change-group")
	c.Check(prompting.AA_MAY_LOCK.String(), Equals, "lock")
	c.Check(prompting.AA_EXEC_MMAP.String(), Equals, "execute-map")
	c.Check(prompting.AA_MAY_LINK.String(), Equals, "link")
	c.Check(prompting.AA_MAY_ONEXEC.String(), Equals, "change-profile-on-exec")
	c.Check(prompting.AA_MAY_CHANGE_PROFILE.String(), Equals, "change-profile")

	// Unknown bits are shown in hex.
	c.Check(prompting.FilePermission(1<<17).String(), Equals, "0x20000")

	// Collection of bits are grouped together in order.
	c.Check((prompting.AA_MAY_READ | prompting.AA_MAY_WRITE).String(), Equals, "write|read")
}

func (*permissionSuite) TestIsValid(c *C) {
	c.Check(prompting.AA_MAY_READ.IsValid(), Equals, true)
	// 1<<17 is not defined in userspace headers
	c.Check(prompting.FilePermission(1<<17).IsValid(), Equals, false)
}
