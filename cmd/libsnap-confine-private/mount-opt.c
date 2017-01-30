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
	char *to = buf;
	unsigned long used = 0;
	to = stpcpy(to, "");
#define F(FLAG, TEXT) do if (flags & (FLAG)) { to = stpcpy(to, #TEXT ","); flags ^= (FLAG); } while (0)
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
			to = stpcpy(to, "rbind,");
			used |= MS_REC;
		} else {
			to = stpcpy(to, "bind,");
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
			to = stpcpy(to, "rprivate,");
			used |= MS_REC;
		} else {
			to = stpcpy(to, "private,");
		}
		flags ^= MS_PRIVATE;
	}
	if (flags & MS_SLAVE) {
		if (flags & MS_REC) {
			to = stpcpy(to, "rslave,");
			used |= MS_REC;
		} else {
			to = stpcpy(to, "slave,");
		}
		flags ^= MS_SLAVE;
	}
	if (flags & MS_SHARED) {
		if (flags & MS_REC) {
			to = stpcpy(to, "rshared,");
			used |= MS_REC;
		} else {
			to = stpcpy(to, "shared,");
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
		to = stpcpy(to, of);
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
	// NOTE: this uses static buffer because it has lower complexity than a
	// dynamically allocated buffer. We've decided as a team to prefer this
	// approach.
	static char buf[PATH_MAX * 2 + 1000];
	char *to = buf;
	int used_special_flags = 0;

	to = stpcpy(to, "mount");

	// Add filesysystem type if it's there and doesn't have the special value "none"
	if (filesystemtype != NULL && strcmp(filesystemtype, "none") != 0) {
		to = stpcpy(to, " -t ");
		to = stpcpy(to, filesystemtype);
	}
	// Check for some special, dedicated syntax. This exists for:
	// - bind mounts (bind)
	//   - including the recursive variant
	if (mountflags & MS_BIND) {
		const char *special = mountflags & MS_REC ?
		    " --rbind" : " --bind";
		used_special_flags |= MS_BIND | MS_REC;
		to = stpcpy(to, special);
	}
	// - moving mount point location (move)
	if (mountflags & MS_MOVE) {
		const char *special = " --move";
		used_special_flags |= MS_MOVE;
		to = stpcpy(to, special);
	}
	// - shared subtree operations (shared, slave, private, unbindable)
	//   - including the recursive variants
	if (MS_SHARED & mountflags) {
		const char *special = mountflags & MS_REC ?
		    " --make-rshared" : " --make-shared";
		used_special_flags |= MS_SHARED | MS_REC;
		to = stpcpy(to, special);
	}
	if (MS_SLAVE & mountflags) {
		const char *special = mountflags & MS_REC ?
		    " --make-rslave" : " --make-slave";
		used_special_flags |= MS_SLAVE | MS_REC;
		to = stpcpy(to, special);
	}
	if (MS_PRIVATE & mountflags) {
		const char *special = mountflags & MS_REC ?
		    " --make-rprivate" : " --make-private";
		used_special_flags |= MS_PRIVATE | MS_REC;
		to = stpcpy(to, special);
	}
	if (MS_UNBINDABLE & mountflags) {
		const char *special = mountflags & MS_REC ?
		    " --make-runbindable" : " --make-unbindable";
		used_special_flags |= MS_UNBINDABLE | MS_REC;
		to = stpcpy(to, special);
	}
	// If regular option syntax exists then use it.
	if (mountflags & ~used_special_flags) {
		const char *regular =
		    sc_mount_opt2str(mountflags & ~used_special_flags);
		to = stpcpy(to, " -o ");
		to = stpcpy(to, regular);
	}
	// Add source and target locations
	if (source != NULL && strcmp(source, "none") != 0) {
		to = stpcpy(to, " ");
		to = stpcpy(to, source);
	}
	if (target != NULL && strcmp(target, "none") != 0) {
		to = stpcpy(to, " ");
		to = stpcpy(to, target);
	}
	return buf;
}

char *sc_umount_cmd(const char *target, int flags)
{
	// NOTE: this uses static buffer because it has lower complexity than a
	// dynamically allocated buffer. We've decided as a team to prefer this
	// approach.
	static char buf[PATH_MAX + 1000];
	char *to = buf;

	to = stpcpy(to, "umount");

	if (flags & MNT_FORCE) {
		to = stpcpy(to, " --force");
	}

	if (flags & MNT_DETACH) {
		to = stpcpy(to, " --lazy");
	}
	if (flags & MNT_EXPIRE) {
		// NOTE: there's no real command line option for MNT_EXPIRE
		to = stpcpy(to, " --expire");
	}
	if (flags & UMOUNT_NOFOLLOW) {
		// NOTE: there's no real command line option for UMOUNT_NOFOLLOW
		to = stpcpy(to, " --no-follow");
	}
	if (target != NULL) {
		to = stpcpy(to, " ");
		to = stpcpy(to, target);
	}
	return buf;
}

void sc_do_mount(const char *source, const char *target,
		 const char *filesystemtype, unsigned long mountflags,
		 const void *data)
{
	char *mount_cmd =
	    sc_mount_cmd(source, target, filesystemtype, mountflags, data);
	debug("performing operation: %s", mount_cmd);
	if (mount(source, target, filesystemtype, mountflags, data) < 0) {
		die("cannot perform operation: %s", mount_cmd);
	}
}

void sc_do_umount(const char *target, int flags)
{
	char *umount_cmd = sc_umount_cmd(target, flags);
	debug("performing operation: %s", umount_cmd);
	if (umount2(target, flags) < 0) {
		die("cannot perform operation: %s", umount_cmd);
	}
}
