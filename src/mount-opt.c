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

#include <stdio.h>
#include <string.h>
#include <sys/mount.h>

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
