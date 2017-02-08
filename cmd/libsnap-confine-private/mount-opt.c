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
#include "string-utils.h"

const char *sc_mount_opt2str(char *buf, size_t buf_size, unsigned long flags)
{
	unsigned long used = 0;
	sc_string_init(buf, buf_size);
#define F(FLAG, TEXT) do if (flags & (FLAG)) { sc_string_append(buf, buf_size, #TEXT ","); flags ^= (FLAG); } while (0)
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
			sc_string_append(buf, buf_size, "rbind,");
			used |= MS_REC;
		} else {
			sc_string_append(buf, buf_size, "bind,");
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
			sc_string_append(buf, buf_size, "rprivate,");
			used |= MS_REC;
		} else {
			sc_string_append(buf, buf_size, "private,");
		}
		flags ^= MS_PRIVATE;
	}
	if (flags & MS_SLAVE) {
		if (flags & MS_REC) {
			sc_string_append(buf, buf_size, "rslave,");
			used |= MS_REC;
		} else {
			sc_string_append(buf, buf_size, "slave,");
		}
		flags ^= MS_SLAVE;
	}
	if (flags & MS_SHARED) {
		if (flags & MS_REC) {
			sc_string_append(buf, buf_size, "rshared,");
			used |= MS_REC;
		} else {
			sc_string_append(buf, buf_size, "shared,");
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
		sc_must_snprintf(of, sizeof of, "%#lx", flags);
		sc_string_append(buf, buf_size, of);
	}
	// Chop the excess comma from the end.
	size_t len = strnlen(buf, buf_size);
	if (len > 0 && buf[len - 1] == ',') {
		buf[len - 1] = 0;
	}
	return buf;
}

const char *sc_mount_cmd(char *buf, size_t buf_size, const char *source, const char
			 *target, const char *filesystemtype,
			 unsigned long mountflags, const
			 void *data)
{
	sc_string_init(buf, buf_size);
	sc_string_append(buf, buf_size, "mount");

	// Add filesysystem type if it's there and doesn't have the special value "none"
	if (filesystemtype != NULL && strcmp(filesystemtype, "none") != 0) {
		sc_string_append(buf, buf_size, " -t ");
		sc_string_append(buf, buf_size, filesystemtype);
	}
	// Check for some special, dedicated syntax. Collect the flags that were
	// displayed this way so that they are not repeated with -o foo syntax.
	int used_special_flags = 0;

	// Bind-ounts (bind)
	if (mountflags & MS_BIND) {
		if (mountflags & MS_REC) {
			sc_string_append(buf, buf_size, " --rbind");
		} else {
			sc_string_append(buf, buf_size, " --bind");
		}
		used_special_flags |= MS_BIND | MS_REC;
	}
	// Moving mount point location (move)
	if (mountflags & MS_MOVE) {
		sc_string_append(buf, buf_size, " --move");
		used_special_flags |= MS_MOVE;
	}
	// Shared subtree operations (shared, slave, private, unbindable).
	if (MS_SHARED & mountflags) {
		if (mountflags & MS_REC) {
			sc_string_append(buf, buf_size, " --make-rshared");
		} else {
			sc_string_append(buf, buf_size, " --make-shared");
		}
		used_special_flags |= MS_SHARED | MS_REC;
	}

	if (MS_SLAVE & mountflags) {
		if (mountflags & MS_REC) {
			sc_string_append(buf, buf_size, " --make-rslave");
		} else {
			sc_string_append(buf, buf_size, " --make-slave");
		}
		used_special_flags |= MS_SLAVE | MS_REC;
	}

	if (MS_PRIVATE & mountflags) {
		if (mountflags & MS_REC) {
			sc_string_append(buf, buf_size, " --make-rprivate");
		} else {
			sc_string_append(buf, buf_size, " --make-private");
		}
		used_special_flags |= MS_PRIVATE | MS_REC;
	}

	if (MS_UNBINDABLE & mountflags) {
		if (mountflags & MS_REC) {
			sc_string_append(buf, buf_size, " --make-runbindable");
		} else {
			sc_string_append(buf, buf_size, " --make-unbindable");
		}
		used_special_flags |= MS_UNBINDABLE | MS_REC;
	}
	// If regular option syntax exists then use it.
	if (mountflags & ~used_special_flags) {
		sc_string_append(buf, buf_size, " -o ");
		size_t used = strnlen(buf, buf_size);
		// NOTE: the option are written directly to the buffer we are working with.
		sc_mount_opt2str(buf + used, buf_size - used,
				 mountflags & ~used_special_flags);
	}
	// Add source and target locations
	if (source != NULL && strcmp(source, "none") != 0) {
		sc_string_append(buf, buf_size, " ");
		sc_string_append(buf, buf_size, source);
	}
	if (target != NULL && strcmp(target, "none") != 0) {
		sc_string_append(buf, buf_size, " ");
		sc_string_append(buf, buf_size, target);
	}

	return buf;
}

const char *sc_umount_cmd(char *buf, size_t buf_size, const char *target,
			  int flags)
{
	sc_string_init(buf, buf_size);
	sc_string_append(buf, buf_size, "umount");

	if (flags & MNT_FORCE) {
		sc_string_append(buf, buf_size, " --force");
	}

	if (flags & MNT_DETACH) {
		sc_string_append(buf, buf_size, " --lazy");
	}
	if (flags & MNT_EXPIRE) {
		// NOTE: there's no real command line option for MNT_EXPIRE
		sc_string_append(buf, buf_size, " --expire");
	}
	if (flags & UMOUNT_NOFOLLOW) {
		// NOTE: there's no real command line option for UMOUNT_NOFOLLOW
		sc_string_append(buf, buf_size, " --no-follow");
	}
	if (target != NULL) {
		sc_string_append(buf, buf_size, " ");
		sc_string_append(buf, buf_size, target);
	}

	return buf;
}
