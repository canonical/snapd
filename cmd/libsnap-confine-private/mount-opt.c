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

#include <errno.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>

#include "fault-injection.h"
#include "privs.h"
#include "string-utils.h"
#include "utils.h"

const char *sc_mount_opt2str(char *buf, size_t buf_size, unsigned long flags)
{
	unsigned long used = 0;
	sc_string_init(buf, buf_size);

#define F(FLAG, TEXT) do {                                         \
    if (flags & (FLAG)) {                                          \
      sc_string_append(buf, buf_size, #TEXT ","); flags ^= (FLAG); \
    }                                                              \
  } while (0)

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
		char of[128] = { 0 };
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
			 *target, const char *fs_type, unsigned long mountflags, const
			 void *data)
{
	sc_string_init(buf, buf_size);
	sc_string_append(buf, buf_size, "mount");

	// Add filesysystem type if it's there and doesn't have the special value "none"
	if (fs_type != NULL && strncmp(fs_type, "none", 5) != 0) {
		sc_string_append(buf, buf_size, " -t ");
		sc_string_append(buf, buf_size, fs_type);
	}
	// Check for some special, dedicated options, that aren't represented with
	// the generic mount option argument (mount -o ...), by collecting those
	// options that we will display as command line arguments in
	// used_special_flags. This is used below to filter out these arguments
	// from mount_flags when calling sc_mount_opt2str().
	int used_special_flags = 0;

	// Bind-ounts (bind)
	if (mountflags & MS_BIND) {
		if (mountflags & MS_REC) {
			sc_string_append(buf, buf_size, " --rbind");
			used_special_flags |= MS_REC;
		} else {
			sc_string_append(buf, buf_size, " --bind");
		}
		used_special_flags |= MS_BIND;
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
			used_special_flags |= MS_REC;
		} else {
			sc_string_append(buf, buf_size, " --make-shared");
		}
		used_special_flags |= MS_SHARED;
	}

	if (MS_SLAVE & mountflags) {
		if (mountflags & MS_REC) {
			sc_string_append(buf, buf_size, " --make-rslave");
			used_special_flags |= MS_REC;
		} else {
			sc_string_append(buf, buf_size, " --make-slave");
		}
		used_special_flags |= MS_SLAVE;
	}

	if (MS_PRIVATE & mountflags) {
		if (mountflags & MS_REC) {
			sc_string_append(buf, buf_size, " --make-rprivate");
			used_special_flags |= MS_REC;
		} else {
			sc_string_append(buf, buf_size, " --make-private");
		}
		used_special_flags |= MS_PRIVATE;
	}

	if (MS_UNBINDABLE & mountflags) {
		if (mountflags & MS_REC) {
			sc_string_append(buf, buf_size, " --make-runbindable");
			used_special_flags |= MS_REC;
		} else {
			sc_string_append(buf, buf_size, " --make-unbindable");
		}
		used_special_flags |= MS_UNBINDABLE;
	}
	// If regular option syntax exists then use it.
	if (mountflags & ~used_special_flags) {
		char opts_buf[1000] = { 0 };
		sc_mount_opt2str(opts_buf, sizeof opts_buf, mountflags &
				 ~used_special_flags);
		sc_string_append(buf, buf_size, " -o ");
		sc_string_append(buf, buf_size, opts_buf);
	}
	// Add source and target locations
	if (source != NULL && strncmp(source, "none", 5) != 0) {
		sc_string_append(buf, buf_size, " ");
		sc_string_append(buf, buf_size, source);
	}
	if (target != NULL && strncmp(target, "none", 5) != 0) {
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

#ifndef SNAP_CONFINE_DEBUG_BUILD
static const char *use_debug_build =
    "(disabled) use debug build to see details";
#endif

static void sc_do_mount_ex(const char *source, const char *target,
			   const char *fs_type,
			   unsigned long mountflags, const void *data,
			   bool optional)
{
	char buf[10000] = { 0 };
	const char *mount_cmd = NULL;

	if (sc_is_debug_enabled()) {
#ifdef SNAP_CONFINE_DEBUG_BUILD
		mount_cmd = sc_mount_cmd(buf, sizeof(buf), source,
					 target, fs_type, mountflags, data);
#else
		mount_cmd = use_debug_build;
#endif
		debug("performing operation: %s", mount_cmd);
	}
	if (sc_faulty("mount", NULL)
	    || mount(source, target, fs_type, mountflags, data) < 0) {
		int saved_errno = errno;
		if (optional && saved_errno == ENOENT) {
			// The special-cased value that is allowed to fail.
			return;
		}
		// Drop privileges so that we can compute our nice error message
		// without risking an attack on one of the string functions there.
		sc_privs_drop();

		// Compute the equivalent mount command.
		mount_cmd = sc_mount_cmd(buf, sizeof(buf), source,
					 target, fs_type, mountflags, data);
		// Restore errno and die.
		errno = saved_errno;
		die("cannot perform operation: %s", mount_cmd);
	}
}

void sc_do_mount(const char *source, const char *target,
		 const char *fs_type, unsigned long mountflags,
		 const void *data)
{
	return sc_do_mount_ex(source, target, fs_type, mountflags, data, false);
}

void sc_do_optional_mount(const char *source, const char *target,
			  const char *fs_type, unsigned long mountflags,
			  const void *data)
{
	return sc_do_mount_ex(source, target, fs_type, mountflags, data, true);
}

void sc_do_umount(const char *target, int flags)
{
	char buf[10000] = { 0 };
	const char *umount_cmd = NULL;

	if (sc_is_debug_enabled()) {
#ifdef SNAP_CONFINE_DEBUG_BUILD
		umount_cmd = sc_umount_cmd(buf, sizeof(buf), target, flags);
#else
		umount_cmd = use_debug_build;
#endif
		debug("performing operation: %s", umount_cmd);
	}
	if (sc_faulty("umount", NULL) || umount2(target, flags) < 0) {
		// Save errno as ensure can clobber it.
		int saved_errno = errno;

		// Drop privileges so that we can compute our nice error message
		// without risking an attack on one of the string functions there.
		sc_privs_drop();

		// Compute the equivalent umount command.
		umount_cmd = sc_umount_cmd(buf, sizeof(buf), target, flags);
		// Restore errno and die.
		errno = saved_errno;
		die("cannot perform operation: %s", umount_cmd);
	}
}
