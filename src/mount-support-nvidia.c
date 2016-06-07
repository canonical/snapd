/*
 * Copyright (C) 2015 Canonical Ltd
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

#include "config.h"
#include "mount-support-nvidia.h"

#ifdef NVIDIA_MOUNT
#include <glob.h>
#include <stdlib.h>
#include <sys/mount.h>

#include "utils.h"
#endif				// ifdef NVIDIA_MOUNT

void sc_bind_mount_nvidia_driver()
{
#ifdef NVIDIA_MOUNT
	// Bind mount the binary nvidia driver into /var/lib/snapd/lib/gl.
	// It is assumed that the driver directory is /usr/lib/nvidia-*.
	// and that only one such directory exists.
	glob_t glob_res __attribute__ ((__cleanup__(globfree))) = {
	.gl_pathv = NULL};
	int err = glob("/usr/lib/nvidia-[1-9][0-9][0-9]/",
		       GLOB_ONLYDIR | GLOB_MARK, NULL,
		       &glob_res);
	if (err != 0 && err != GLOB_NOMATCH) {
		die("cannot for nvidia drivers: %d", err);
	}
	switch (glob_res.gl_pathc) {
	case 0:
		debug("cannot find any nvidia drivers");
		break;
	case 1:;
		const char *src = glob_res.gl_pathv[0];
		const char *dst = "/var/lib/snapd/lib/gl";
		debug("bind mounting nvidia driver %s -> %s", src, dst);
		if (mount(src, dst, NULL, MS_BIND, NULL) != 0) {
			die("cannot bind mount nvidia driver %s -> %s",
			    src, dst);
		}
		break;
	default:
		die("multiple nvidia drivers detected, ");
		break;
	}
#endif				// ifdef NVIDIA_MOUNT
}
