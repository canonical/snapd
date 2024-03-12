package notify

import (
	"fmt"
	"strings"
)

// FilePermission is a bit-mask of apparmor permissions in relation to files.
// It is applicable to messages with the class of AA_CLASS_FILE.
type FilePermission uint32

const (
	// AA_MAY_EXEC implies that a process has a permission to execute another
	// program. The specific details of the program are not conveyed.
	AA_MAY_EXEC FilePermission = 1 << iota
	// AA_MAY_WRITE implies that a process may write to a file or socket, or
	// may modify directory contents.
	AA_MAY_WRITE
	// AA_MAY_READ implies that a process may read from a file or socket, or
	// may enumerate directory contents.
	AA_MAY_READ
	// AA_MAY_APPEND implies that a process may open a file in append mode.
	AA_MAY_APPEND
	// AA_MAY_CREATE implies that a process may create a new file.
	AA_MAY_CREATE
	// AA_MAY_DELETE implies that a process may delete a file, directory,
	// symbolic link or socket.
	AA_MAY_DELETE
	// AA_MAY_OPEN implies that a process may open a file or directory. The
	// additional presence of AA_MAY_WRITE or AA_MAY_READ grants specific type
	// of access.
	AA_MAY_OPEN
	// AA_MAY_RENAME implies that a process may rename a file.
	AA_MAY_RENAME
	// AA_MAY_SETATTR is not checked by the kernel.
	AA_MAY_SETATTR
	// AA_MAY_GETATTR is not checked by the kernel.
	AA_MAY_GETATTR
	// AA_MAY_SETCRED is not used in the kernel.
	AA_MAY_SETCRED
	// AA_MAY_GETCRED is not used in the kernel.
	AA_MAY_GETCRED
	// AA_MAY_CHMOD implies that a process may change UNIX file permissions.
	AA_MAY_CHMOD
	// AA_MAY_CHOWN implies that a process may change file ownership.
	AA_MAY_CHOWN
	// AA_MAY_CHGRP implies that a process may change the group ownership of a
	// file. The C-level macro is not defined in any userspace header but is
	// already supported and reported by the kernel.
	AA_MAY_CHGRP
	// AA_MAY_LOCK implies that a process may perform fcntl locking operations
	// on a file.
	AA_MAY_LOCK
	// AA_EXEC_MMAP implies that a process may execute code from an page
	// memory-mapped from a file.
	AA_EXEC_MMAP

	// There are additional permissions defined in the kernel but it seems some
	// of those are unused and their exact scope and meaning is unclear.

	// AA_MAY_LINK implies that a process may create a hard link. Their
	// associated file information describes the hard link name, not the
	// original file.
	AA_MAY_LINK FilePermission = 1 << 18
	// AA_MAY_ONEXEC implies that a process may change the apparmor profile on
	// the next exec call.
	AA_MAY_ONEXEC FilePermission = 1 << 29
	// AA_MAY_CHANGE_PROFILE implies that a process may change the apparmor
	// profile on demand.
	AA_MAY_CHANGE_PROFILE FilePermission = 1 << 30
)

const filePermissionMask = (AA_MAY_EXEC | AA_MAY_WRITE | AA_MAY_READ |
	AA_MAY_APPEND | AA_MAY_CREATE | AA_MAY_DELETE | AA_MAY_OPEN |
	AA_MAY_RENAME | AA_MAY_SETATTR | AA_MAY_GETATTR | AA_MAY_SETCRED |
	AA_MAY_GETCRED | AA_MAY_CHMOD | AA_MAY_CHOWN | AA_MAY_CHGRP |
	AA_MAY_LOCK | AA_EXEC_MMAP | AA_MAY_LINK | AA_MAY_ONEXEC |
	AA_MAY_CHANGE_PROFILE)

// String returns readable representation of the file permission value.
func (p FilePermission) String() string {
	frags := make([]string, 0, 21)
	if p&AA_MAY_EXEC != 0 {
		frags = append(frags, "execute")
	}
	if p&AA_MAY_WRITE != 0 {
		frags = append(frags, "write")
	}
	if p&AA_MAY_READ != 0 {
		frags = append(frags, "read")
	}
	if p&AA_MAY_APPEND != 0 {
		frags = append(frags, "append")
	}
	if p&AA_MAY_CREATE != 0 {
		frags = append(frags, "create")
	}
	if p&AA_MAY_DELETE != 0 {
		frags = append(frags, "delete")
	}
	if p&AA_MAY_OPEN != 0 {
		frags = append(frags, "open")
	}
	if p&AA_MAY_RENAME != 0 {
		frags = append(frags, "rename")
	}
	if p&AA_MAY_SETATTR != 0 {
		frags = append(frags, "set-attr")
	}
	if p&AA_MAY_GETATTR != 0 {
		frags = append(frags, "get-attr")
	}
	if p&AA_MAY_SETCRED != 0 {
		frags = append(frags, "set-cred")
	}
	if p&AA_MAY_GETCRED != 0 {
		frags = append(frags, "get-cred")
	}
	if p&AA_MAY_CHMOD != 0 {
		frags = append(frags, "change-mode")
	}
	if p&AA_MAY_CHOWN != 0 {
		frags = append(frags, "change-owner")
	}
	if p&AA_MAY_CHGRP != 0 {
		frags = append(frags, "change-group")
	}
	if p&AA_MAY_LOCK != 0 {
		frags = append(frags, "lock")
	}
	if p&AA_EXEC_MMAP != 0 {
		frags = append(frags, "execute-map")
	}
	if p&AA_MAY_LINK != 0 {
		frags = append(frags, "link")
	}
	if p&AA_MAY_ONEXEC != 0 {
		frags = append(frags, "change-profile-on-exec")
	}
	if p&AA_MAY_CHANGE_PROFILE != 0 {
		frags = append(frags, "change-profile")
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
