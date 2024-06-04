package notify_test

import (
	"github.com/snapcore/snapd/sandbox/apparmor/notify"

	. "gopkg.in/check.v1"
)

type permissionSuite struct{}

var _ = Suite(&permissionSuite{})

func (*permissionSuite) TestExactValues(c *C) {
	// The specific values must match sys/apparmor.h
	c.Check(notify.AA_MAY_EXEC, Equals, notify.FilePermission(1<<0))
	c.Check(notify.AA_MAY_WRITE, Equals, notify.FilePermission(1<<1))
	c.Check(notify.AA_MAY_READ, Equals, notify.FilePermission(1<<2))
	c.Check(notify.AA_MAY_APPEND, Equals, notify.FilePermission(1<<3))
	c.Check(notify.AA_MAY_CREATE, Equals, notify.FilePermission(1<<4))
	c.Check(notify.AA_MAY_DELETE, Equals, notify.FilePermission(1<<5))
	c.Check(notify.AA_MAY_OPEN, Equals, notify.FilePermission(1<<6))
	c.Check(notify.AA_MAY_RENAME, Equals, notify.FilePermission(1<<7))
	c.Check(notify.AA_MAY_SETATTR, Equals, notify.FilePermission(1<<8))
	c.Check(notify.AA_MAY_GETATTR, Equals, notify.FilePermission(1<<9))
	c.Check(notify.AA_MAY_SETCRED, Equals, notify.FilePermission(1<<10))
	c.Check(notify.AA_MAY_GETCRED, Equals, notify.FilePermission(1<<11))
	c.Check(notify.AA_MAY_CHMOD, Equals, notify.FilePermission(1<<12))
	c.Check(notify.AA_MAY_CHOWN, Equals, notify.FilePermission(1<<13))
	c.Check(notify.AA_MAY_CHGRP, Equals, notify.FilePermission(1<<14))
	c.Check(notify.AA_MAY_LOCK, Equals, notify.FilePermission(0x8000))
	c.Check(notify.AA_EXEC_MMAP, Equals, notify.FilePermission(0x10000))
	c.Check(notify.AA_MAY_LINK, Equals, notify.FilePermission(0x40000))
	c.Check(notify.AA_MAY_ONEXEC, Equals, notify.FilePermission(0x20000000))
	c.Check(notify.AA_MAY_CHANGE_PROFILE, Equals, notify.FilePermission(0x40000000))
}

func (*permissionSuite) TestFilePermissionString(c *C) {
	// No permission bits set.
	c.Check(notify.FilePermission(0).String(), Equals, "none")

	// Specific single permission bit set.
	c.Check(notify.AA_MAY_EXEC.String(), Equals, "execute")
	c.Check(notify.AA_MAY_WRITE.String(), Equals, "write")
	c.Check(notify.AA_MAY_READ.String(), Equals, "read")
	c.Check(notify.AA_MAY_APPEND.String(), Equals, "append")
	c.Check(notify.AA_MAY_CREATE.String(), Equals, "create")
	c.Check(notify.AA_MAY_DELETE.String(), Equals, "delete")
	c.Check(notify.AA_MAY_OPEN.String(), Equals, "open")
	c.Check(notify.AA_MAY_RENAME.String(), Equals, "rename")
	c.Check(notify.AA_MAY_SETATTR.String(), Equals, "set-attr")
	c.Check(notify.AA_MAY_GETATTR.String(), Equals, "get-attr")
	c.Check(notify.AA_MAY_SETCRED.String(), Equals, "set-cred")
	c.Check(notify.AA_MAY_GETCRED.String(), Equals, "get-cred")
	c.Check(notify.AA_MAY_CHMOD.String(), Equals, "change-mode")
	c.Check(notify.AA_MAY_CHOWN.String(), Equals, "change-owner")
	c.Check(notify.AA_MAY_CHGRP.String(), Equals, "change-group")
	c.Check(notify.AA_MAY_LOCK.String(), Equals, "lock")
	c.Check(notify.AA_EXEC_MMAP.String(), Equals, "execute-map")
	c.Check(notify.AA_MAY_LINK.String(), Equals, "link")
	c.Check(notify.AA_MAY_ONEXEC.String(), Equals, "change-profile-on-exec")
	c.Check(notify.AA_MAY_CHANGE_PROFILE.String(), Equals, "change-profile")

	// Unknown bits are shown in hex.
	c.Check(notify.FilePermission(1<<17).String(), Equals, "0x20000")

	// Collection of bits are grouped together in order.
	c.Check((notify.AA_MAY_READ | notify.AA_MAY_WRITE).String(), Equals, "write|read")
}

func (*permissionSuite) TestIsValid(c *C) {
	c.Check(notify.AA_MAY_READ.IsValid(), Equals, true)
	// 1<<17 is not defined in userspace headers
	c.Check(notify.FilePermission(1<<17).IsValid(), Equals, false)
}
