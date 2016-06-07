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
	// The driver can be in one of a few locations. On some distributions
	// it is /usr/lib/nvidia-{xxx} (where xxx is the version number)
	// on other distributions it is just /usr/lib/nvidia.
	// Before this is all made easy by snapd and the mount security backend
	// we just look in all the possible places.
	const char *patterns[] = {
		"/usr/lib/nvidia",
		"/usr/lib/nvidia-[1-9][0-9][0-9]",
	};
	glob_t glob_res __attribute__ ((__cleanup__(globfree))) = {
	.gl_pathv = NULL};
	for (int i = 0; i < sizeof patterns / sizeof *patterns; ++i) {
		int err = glob(patterns[i],
			       GLOB_ONLYDIR | GLOB_MARK | (i >
							   0 ? GLOB_APPEND : 0),
			       NULL, &glob_res);
		debug("glob(%s, ...) returned %d", patterns[i], err);
	}
	switch (glob_res.gl_pathc) {
	case 0:
		debug("cannot find any nvidia drivers");
		break;
	case 1:;
		// Bind mount the binary nvidia driver into /var/lib/snapd/lib/gl.
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
