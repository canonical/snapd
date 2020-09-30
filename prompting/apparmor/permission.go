package apparmor

import (
	"fmt"
	"strings"
)

// FilePermission is a bit-mask of apparmor permissions in relation to files.
// It is applicable to messages with the class of MediationClassFile.
type FilePermission uint32

const (
	// MayExecutePermission corresponds to AA_MAY_EXEC and implies that a
	// process has a permission to execute another program. The specific details
	// of the program are not conveyed.
	MayExecutePermission FilePermission = 1 << iota
	// MayWritePermission corresponds to AA_MAY_WRITE and implies that a process
	// may write to a file or socket, or may modify directory contents.
	MayWritePermission
	// MayReadPermission corresponds to AA_MAY_READ and implies that a process
	// may read from a file or socket, or may enumerate directory contents.
	MayReadPermission
	// MayAppendPermission corresponds to AA_MAY_APPEND and implies that a process
	// may open a file in append mode.
	MayAppendPermission
	// MayCreatePermission corresponds to AA_MAY_CREATE and implies that a
	// process may create a new file.
	MayCreatePermission
	// MayDeletePermission corresponds to AA_MAY_DELETE and implies that a
	// process may delete a file, directory, symbolic link or socket.
	MayDeletePermission
	// MayOpenPermission corresponds to AA_MAY_OPEN and implies that a process
	// may open a file or directory. The additional presence of
	// MayWritePermission or MayReadPermission grants specific type of access.
	MayOpenPermission
	// MayRenamePermission corresponds to AA_MAY_RENAME and implies that a
	// process may rename a file.
	MayRenamePermission
	// MaySetAttrPermission corresponds to AA_MAY_SETATTR and is not checked by
	// the kernel.
	MaySetAttrPermission
	// MayGetAttrPermission corresponds to AA_MAY_GETATTR and is not checked by
	// the kernel.
	MayGetAttrPermission
	// MaySetCredentialPermission corresponds to AA_MAY_SETCRED and is not used
	// in the kernel.
	MaySetCredentialPermission
	// MayGetCredentialPermission corresponds to AA_MAY_GETCRED and is not used
	// in the kernel.
	MayGetCredentialPermission
	// MayChangeModePermission corresponds to AA_MAY_CHMOD and implies that a
	// process may change UNIX file permissions.
	MayChangeModePermission
	// MayChangeOwnerPermission corresponds to AA_MAY_CHOWN and implies that a
	// process may change file ownership.
	MayChangeOwnerPermission
	// MayChangeGroupPermission corresponds to AA_MAY_CHGRP and implies that a
	// process may change the group ownership of a file. The C-level macro is
	// not defined in any userspace header but is already supported and reported
	// by the kernel.
	MayChangeGroupPermission
	// MayLockPermission corresponds to AA_MAY_LOCK and implies that a process
	// may perform fcntl locking operations on a file.
	MayLockPermission
	// MayExecuteMapPermission corresponds to AA_EXEC_MMAP and implies that a
	// process may execute code from an page memory-mapped from a file.
	MayExecuteMapPermission

	// There are additional permissions defined in the kernel but it seems some
	// of those are unused and their exact scope and meaning is unclear.

	// MayLinkPermission corresponds to AA_MAY_LINK and implies that a process
	// may create a hard link. There associated file information describes the
	// hard link name, not the original file.
	MayLinkPermission FilePermission = 1 << 18
	// MayChangeProfileOnExecPermission corresponds to AA_MAY_ONEXEC and implies
	// that a process may change the apparmor profile on the next exec call.
	MayChangeProfileOnExecPermission FilePermission = 1 << 29
	// MayChangeProfilePermission corresponds to AA_MAY_CHANGE_PROFILE and
	// implies that a process may change the apparmor profile on demand.
	MayChangeProfilePermission FilePermission = 1 << 30
)

const filePermissionMask = (MayExecutePermission | MayWritePermission | MayReadPermission |
	MayAppendPermission | MayCreatePermission | MayDeletePermission |
	MayOpenPermission | MayRenamePermission | MaySetAttrPermission |
	MayGetAttrPermission | MaySetCredentialPermission | MayGetCredentialPermission |
	MayChangeModePermission | MayChangeOwnerPermission | MayChangeGroupPermission |
	MayLockPermission | MayExecuteMapPermission | MayLinkPermission |
	MayChangeProfileOnExecPermission | MayChangeProfilePermission)

// String returns readable representation of the file permission value.
func (p FilePermission) String() string {
	frags := make([]string, 0, 21)
	if p&MayExecutePermission != 0 {
		frags = append(frags, "execute")
	}
	if p&MayWritePermission != 0 {
		frags = append(frags, "write")
	}
	if p&MayReadPermission != 0 {
		frags = append(frags, "read")
	}
	if p&MayAppendPermission != 0 {
		frags = append(frags, "append")
	}
	if p&MayCreatePermission != 0 {
		frags = append(frags, "create")
	}
	if p&MayDeletePermission != 0 {
		frags = append(frags, "delete")
	}
	if p&MayOpenPermission != 0 {
		frags = append(frags, "open")
	}
	if p&MayRenamePermission != 0 {
		frags = append(frags, "rename")
	}
	if p&MaySetAttrPermission != 0 {
		frags = append(frags, "set-attr")
	}
	if p&MayGetAttrPermission != 0 {
		frags = append(frags, "get-attr")
	}
	if p&MaySetCredentialPermission != 0 {
		frags = append(frags, "set-cred")
	}
	if p&MayGetCredentialPermission != 0 {
		frags = append(frags, "get-cred")
	}
	if p&MayChangeModePermission != 0 {
		frags = append(frags, "change-mode")
	}
	if p&MayChangeOwnerPermission != 0 {
		frags = append(frags, "change-owner")
	}
	if p&MayChangeGroupPermission != 0 {
		frags = append(frags, "change-group")
	}
	if p&MayLockPermission != 0 {
		frags = append(frags, "lock")
	}
	if p&MayExecuteMapPermission != 0 {
		frags = append(frags, "execute-map")
	}
	if p&MayLinkPermission != 0 {
		frags = append(frags, "link")
	}
	if p&MayChangeProfilePermission != 0 {
		frags = append(frags, "change-profile")
	}
	if p&MayChangeProfileOnExecPermission != 0 {
		frags = append(frags, "change-profile-on-exec")
	}
	if residue := p &^ filePermissionMask; residue != 0 {
		frags = append(frags, fmt.Sprintf("%#x", uint(residue)))
	}
	if len(frags) == 0 {
		return "none"
	}
	return strings.Join(frags, "|")
}

// IsValid returns true if the given file permission contains only known bits set.
func (p FilePermission) IsValid() bool {
	return p & ^filePermissionMask == 0
}
