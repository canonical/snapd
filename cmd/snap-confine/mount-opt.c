/*
 * Copyright (C) 2016 Canonical Ltd
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

#include "mount-opt.h"

#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>

#include "utils.h"

const char *sc_mount_opt2str(unsigned long flags)
{
	static char buf[1000];
	unsigned long used = 0;
	strcpy(buf, "");
#define F(FLAG, TEXT) do if (flags & (FLAG)) { strcat(buf, #TEXT ","); flags ^= (FLAG); } while (0)
	F(MS_RDONLY, ro);
	F(MS_NOSUID, nosuid);
	F(MS_NODEV, nodev);
	F(MS_NOEXEC, noexec);
	F(MS_SYNCHRONOUS, sync);
	F(MS_REMOUNT, remount);
	F(MS_MANDLOCK, mand);
	F(MS_DIRSYNC, dirsync);
	F(MS_NOATIME, noatime);
	F(MS_NODIRATIME, nodiratime);
	if (flags & MS_BIND) {
		if (flags & MS_REC) {
			strcat(buf, "rbind,");
			used |= MS_REC;
		} else {
			strcat(buf, "bind,");
		}
		flags ^= MS_BIND;
	}
	F(MS_MOVE, move);
	// The MS_REC flag handled separately by affected flags (MS_BIND,
	// MS_PRIVATE, MS_SLAVE, MS_SHARED)
	// XXX: kernel has MS_VERBOSE, glibc has MS_SILENT, both use the same constant
	F(MS_SILENT, silent);
	F(MS_POSIXACL, acl);
	F(MS_UNBINDABLE, unbindable);
	if (flags & MS_PRIVATE) {
		if (flags & MS_REC) {
			strcat(buf, "rprivate,");
			used |= MS_REC;
		} else {
			strcat(buf, "private,");
		}
		flags ^= MS_PRIVATE;
	}
	if (flags & MS_SLAVE) {
		if (flags & MS_REC) {
			strcat(buf, "rslave,");
			used |= MS_REC;
		} else {
			strcat(buf, "slave,");
		}
		flags ^= MS_SLAVE;
	}
	if (flags & MS_SHARED) {
		if (flags & MS_REC) {
			strcat(buf, "rshared,");
			used |= MS_REC;
		} else {
			strcat(buf, "shared,");
		}
		flags ^= MS_SHARED;
	}
	flags ^= used;		// this is just for MS_REC
	F(MS_RELATIME, relatime);
	F(MS_KERNMOUNT, kernmount);
	F(MS_I_VERSION, iversion);
	F(MS_STRICTATIME, strictatime);
#ifndef MS_LAZYTIME
#define MS_LAZYTIME (1<<25)
#endif
	F(MS_LAZYTIME, lazytime);
#ifndef MS_NOSEC
#define MS_NOSEC (1 << 28)
#endif
	F(MS_NOSEC, nosec);
#ifndef MS_BORN
#define MS_BORN (1 << 29)
#endif
	F(MS_BORN, born);
	F(MS_ACTIVE, active);
	F(MS_NOUSER, nouser);
#undef F
	// Render any flags that are unaccounted for.
	if (flags) {
		char of[128];
		sprintf(of, "%#lx", flags);
		strcat(buf, of);
	}
	// Chop the excess comma from the end.
	size_t len = strlen(buf);
	if (len > 0 && buf[len - 1] == ',') {
		buf[len - 1] = 0;
	}
	return buf;
}

char *sc_mount_cmd(const char *source, const char *target,
		   const char *filesystemtype, unsigned long mountflags,
		   const void *data)
{
	char buf[100 + PATH_MAX * 2];
	char *special = NULL;
	int used_special_flags = 0;
	strcpy(buf, "mount");

	// Check for some special, dedicated syntax. This exists for:
	// - bind mounts (bind)
	//   - including the recursive variant
	if (mountflags & MS_BIND) {
		if (mountflags & MS_REC) {
			special = " --rbind";
		} else {
			special = " --bind";
		}
		used_special_flags |= MS_BIND | MS_REC;
	} else if (mountflags & MS_MOVE) {
		// - moving mount point location (move)
		special = " --move";
		used_special_flags |= MS_MOVE;
	} else if (MS_SHARED & mountflags) {
		// - shared subtree operations (shared, slave, private, unbindable)
		//   - including the recursive variants
		if (mountflags & MS_REC) {
			special = " --make-rshared";
		} else {
			special = " --make-shared";
		}
		used_special_flags |= MS_SHARED | MS_REC;
	} else if (MS_SLAVE & mountflags) {
		if (mountflags & MS_REC) {
			special = " --make-rslave";
		} else {
			special = " --make-slave";
		}
		used_special_flags |= MS_SLAVE | MS_REC;
	} else if (MS_PRIVATE & mountflags) {
		if (mountflags & MS_REC) {
			special = " --make-rprivate";
		} else {
			special = " --make-private";
		}
		used_special_flags |= MS_PRIVATE | MS_REC;
	} else if (MS_UNBINDABLE & mountflags) {
		if (mountflags & MS_REC) {
			special = " --make-runbindable";
		} else {
			special = " --make-unbindable";
		}
		used_special_flags |= MS_UNBINDABLE | MS_REC;
	}
	// Add filesysystem type if it's there and doesn't have the special value "none"
	if (filesystemtype != NULL && strcmp(filesystemtype, "none") != 0) {
		strncat(buf, " -t ", sizeof buf - 1);
		strncat(buf, filesystemtype, sizeof buf - 1);
	}
	// If special option syntax exists then use it.
	if (special != NULL) {
		strncat(buf, special, sizeof buf - 1);
	}
	// If regular option syntax exists then use it.
	if (mountflags & ~used_special_flags) {
		const char *regular =
		    sc_mount_opt2str(mountflags & ~used_special_flags);
		strncat(buf, " -o ", sizeof buf - 1);
		strncat(buf, regular, sizeof buf - 1);
	}
	// Add source and target locations
	if (source != NULL && strcmp(source, "none") != 0) {
		strncat(buf, " ", sizeof buf - 1);
		strncat(buf, source, sizeof buf - 1);
	}
	if (target != NULL && strcmp(target, "none") != 0) {
		strncat(buf, " ", sizeof buf - 1);
		strncat(buf, target, sizeof buf - 1);
	}
	// We're done, just copy the buf
	char *buf_copy = strdup(buf);
	if (buf_copy == NULL) {
		die("cannot copy memory buffer");
	}
	return buf_copy;
}

char *sc_umount_cmd(const char *target, int flags)
{
	char buf[100 + PATH_MAX];
	strcpy(buf, "umount");

	if (flags & MNT_FORCE) {
		strncat(buf, " --force", sizeof buf - 1);
	}

	if (flags & MNT_DETACH) {
		strncat(buf, " --lazy", sizeof buf - 1);
	}
	if (flags & MNT_EXPIRE) {
		// NOTE: there's no real command line option for MNT_EXPIRE
		strncat(buf, " --expire", sizeof buf - 1);
	}
	if (flags & UMOUNT_NOFOLLOW) {
		// NOTE: there's no real command line option for UMOUNT_NOFOLLOW
		strncat(buf, " --no-follow", sizeof buf - 1);
	}
	if (target != NULL) {
		strncat(buf, " ", sizeof buf - 1);
		strncat(buf, target, sizeof buf - 1);
	}
	// We're done, just copy the buf
	char *buf_copy = strdup(buf);
	if (buf_copy == NULL) {
		die("cannot copy memory buffer");
	}
	return buf_copy;
}
