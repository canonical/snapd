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

static void sc_grow_string(char **s, const char *extra)
{
	size_t extra_len = extra != NULL ? strlen(extra) : 0;
	size_t initial_len = *s != NULL ? strlen(*s) : 0;
	if (extra_len == 0) {
		return;
	}
	char *result = realloc(*s, initial_len + extra_len + 1);
	if (result == NULL) {
		die("cannot grow string by %zd bytes to %zd bytes",
		    initial_len + 1, initial_len + extra_len + 1);
	}
	if (extra != NULL) {
		memcpy(result + initial_len, extra, extra_len + 1);
	}
	*s = result;
}

char *sc_mount_cmd(const char *source, const char *target,
		   const char *filesystemtype, unsigned long mountflags,
		   const void *data)
{
	char *buf = NULL;
	int used_special_flags = 0;

	sc_grow_string(&buf, "mount");

	// Add filesysystem type if it's there and doesn't have the special value "none"
	if (filesystemtype != NULL && strcmp(filesystemtype, "none") != 0) {
		sc_grow_string(&buf, " -t ");
		sc_grow_string(&buf, filesystemtype);
	}
	// Check for some special, dedicated syntax. This exists for:
	// - bind mounts (bind)
	//   - including the recursive variant
	if (mountflags & MS_BIND) {
		const char *special = mountflags & MS_REC ?
		    " --rbind" : " --bind";
		used_special_flags |= MS_BIND | MS_REC;
		sc_grow_string(&buf, special);
	}
	// - moving mount point location (move)
	if (mountflags & MS_MOVE) {
		const char *special = " --move";
		used_special_flags |= MS_MOVE;
		sc_grow_string(&buf, special);
	}
	// - shared subtree operations (shared, slave, private, unbindable)
	//   - including the recursive variants
	if (MS_SHARED & mountflags) {
		const char *special = mountflags & MS_REC ?
		    " --make-rshared" : " --make-shared";
		used_special_flags |= MS_SHARED | MS_REC;
		sc_grow_string(&buf, special);
	}
	if (MS_SLAVE & mountflags) {
		const char *special = mountflags & MS_REC ?
		    " --make-rslave" : " --make-slave";
		used_special_flags |= MS_SLAVE | MS_REC;
		sc_grow_string(&buf, special);
	}
	if (MS_PRIVATE & mountflags) {
		const char *special = mountflags & MS_REC ?
		    " --make-rprivate" : " --make-private";
		used_special_flags |= MS_PRIVATE | MS_REC;
		sc_grow_string(&buf, special);
	}
	if (MS_UNBINDABLE & mountflags) {
		const char *special = mountflags & MS_REC ?
		    " --make-runbindable" : " --make-unbindable";
		used_special_flags |= MS_UNBINDABLE | MS_REC;
		sc_grow_string(&buf, special);
	}
	// If regular option syntax exists then use it.
	if (mountflags & ~used_special_flags) {
		const char *regular =
		    sc_mount_opt2str(mountflags & ~used_special_flags);
		sc_grow_string(&buf, " -o ");
		sc_grow_string(&buf, regular);
	}
	// Add source and target locations
	if (source != NULL && strcmp(source, "none") != 0) {
		sc_grow_string(&buf, " ");
		sc_grow_string(&buf, source);
	}
	if (target != NULL && strcmp(target, "none") != 0) {
		sc_grow_string(&buf, " ");
		sc_grow_string(&buf, target);
	}
	return buf;
}

char *sc_umount_cmd(const char *target, int flags)
{
	char *buf = NULL;
	sc_grow_string(&buf, "umount");

	if (flags & MNT_FORCE) {
		sc_grow_string(&buf, " --force");
	}

	if (flags & MNT_DETACH) {
		sc_grow_string(&buf, " --lazy");
	}
	if (flags & MNT_EXPIRE) {
		// NOTE: there's no real command line option for MNT_EXPIRE
		sc_grow_string(&buf, " --expire");
	}
	if (flags & UMOUNT_NOFOLLOW) {
		// NOTE: there's no real command line option for UMOUNT_NOFOLLOW
		sc_grow_string(&buf, " --no-follow");
	}
	if (target != NULL) {
		sc_grow_string(&buf, " ");
		sc_grow_string(&buf, target);
	}
	return buf;
}
