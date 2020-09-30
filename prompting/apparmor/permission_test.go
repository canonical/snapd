package apparmor_test

import (
	"github.com/snapcore/cerberus/apparmor"

	. "gopkg.in/check.v1"
)

type permissionSuite struct{}

var _ = Suite(&permissionSuite{})

func (*permissionSuite) TestExactValues(c *C) {
	// The specific values must match sys/apparmor.h
	c.Check(apparmor.MayExecutePermission, Equals, apparmor.FilePermission(1<<0))
	c.Check(apparmor.MayWritePermission, Equals, apparmor.FilePermission(1<<1))
	c.Check(apparmor.MayReadPermission, Equals, apparmor.FilePermission(1<<2))
	c.Check(apparmor.MayAppendPermission, Equals, apparmor.FilePermission(1<<3))
	c.Check(apparmor.MayCreatePermission, Equals, apparmor.FilePermission(1<<4))
	c.Check(apparmor.MayDeletePermission, Equals, apparmor.FilePermission(1<<5))
	c.Check(apparmor.MayOpenPermission, Equals, apparmor.FilePermission(1<<6))
	c.Check(apparmor.MayRenamePermission, Equals, apparmor.FilePermission(1<<7))
	c.Check(apparmor.MaySetAttrPermission, Equals, apparmor.FilePermission(1<<8))
	c.Check(apparmor.MayGetAttrPermission, Equals, apparmor.FilePermission(1<<9))
	c.Check(apparmor.MaySetCredentialPermission, Equals, apparmor.FilePermission(1<<10))
	c.Check(apparmor.MayGetCredentialPermission, Equals, apparmor.FilePermission(1<<11))
	c.Check(apparmor.MayChangeModePermission, Equals, apparmor.FilePermission(1<<12))
	c.Check(apparmor.MayChangeOwnerPermission, Equals, apparmor.FilePermission(1<<13))
	c.Check(apparmor.MayChangeGroupPermission, Equals, apparmor.FilePermission(1<<14))
	c.Check(apparmor.MayLockPermission, Equals, apparmor.FilePermission(0x8000))
	c.Check(apparmor.MayExecuteMapPermission, Equals, apparmor.FilePermission(0x10000))
	c.Check(apparmor.MayLinkPermission, Equals, apparmor.FilePermission(0x40000))
	c.Check(apparmor.MayChangeProfileOnExecPermission, Equals, apparmor.FilePermission(0x20000000))
	c.Check(apparmor.MayChangeProfilePermission, Equals, apparmor.FilePermission(0x40000000))
}

func (*permissionSuite) TestFilePermissionString(c *C) {
	// No permission bits set.
	c.Check(apparmor.FilePermission(0).String(), Equals, "none")

	// Specific single permission bit set.
	c.Check(apparmor.MayExecutePermission.String(), Equals, "execute")
	c.Check(apparmor.MayWritePermission.String(), Equals, "write")
	c.Check(apparmor.MayReadPermission.String(), Equals, "read")
	c.Check(apparmor.MayAppendPermission.String(), Equals, "append")
	c.Check(apparmor.MayCreatePermission.String(), Equals, "create")
	c.Check(apparmor.MayDeletePermission.String(), Equals, "delete")
	c.Check(apparmor.MayOpenPermission.String(), Equals, "open")
	c.Check(apparmor.MayRenamePermission.String(), Equals, "rename")
	c.Check(apparmor.MaySetAttrPermission.String(), Equals, "set-attr")
	c.Check(apparmor.MayGetAttrPermission.String(), Equals, "get-attr")
	c.Check(apparmor.MaySetCredentialPermission.String(), Equals, "set-cred")
	c.Check(apparmor.MayGetCredentialPermission.String(), Equals, "get-cred")
	c.Check(apparmor.MayChangeModePermission.String(), Equals, "change-mode")
	c.Check(apparmor.MayChangeOwnerPermission.String(), Equals, "change-owner")
	c.Check(apparmor.MayChangeGroupPermission.String(), Equals, "change-group")
	c.Check(apparmor.MayLockPermission.String(), Equals, "lock")
	c.Check(apparmor.MayExecuteMapPermission.String(), Equals, "execute-map")
	c.Check(apparmor.MayLinkPermission.String(), Equals, "link")
	c.Check(apparmor.MayChangeProfileOnExecPermission.String(), Equals, "change-profile-on-exec")
	c.Check(apparmor.MayChangeProfilePermission.String(), Equals, "change-profile")

	// Unknown bits are shown in hex.
	c.Check(apparmor.FilePermission(1<<17).String(), Equals, "0x20000")

	// Collection of bits are groupped together
	c.Check((apparmor.MayReadPermission | apparmor.MayWritePermission).String(), Equals, "write|read")
}

func (*permissionSuite) TestIsValid(c *C) {
	c.Check(apparmor.MayReadPermission.IsValid(), Equals, true)
	// 1<<17 is not defined in userspace headers
	c.Check(apparmor.FilePermission(1<<17).IsValid(), Equals, false)
}
