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

#include <stdbool.h>		// bools
#include <stdarg.h>		// va_*
#include <sys/mount.h>		// umount
#include <sys/stat.h>		// mkdir
#include <unistd.h>		// getpid, close
#include <stdlib.h>		// exit
#include <stdio.h>		// fprintf, stderr
#include <sys/ioctl.h>		// ioctl
#include <linux/loop.h>		// LOOP_CLR_FD
#include <sys/reboot.h>		// reboot, RB_*
#include <fcntl.h>		// open
#include <errno.h>		// errno, sys_errlist

#include "system-shutdown-utils.h"
#include "../libsnap-wrap-private/string-utils.h"

int main(int argc, char *argv[])
{
	errno = 0;
	if (getpid() != 1) {
		fprintf(stderr,
			"This is a shutdown helper program; don't call it directly.\n");
		exit(1);
	}

	kmsg("started.");

	/*
	   This program is started by systemd exec'ing the "shutdown" binary
	   inside what used to be /run/initramfs. That is: the system's
	   /run/initramfs is now /, and the old / is now /oldroot. Our job is
	   to disentagle /oldroot and /oldroot/writable, which contain each
	   other in the "live" system. We do this by creating a new /writable
	   and moving the old mount there, previous to which we need to unmount
	   as much as we can. Having done that we should be able to detach the
	   oldroot loop device and finally unmount writable itself.
	 */

	if (mkdir("/writable", 0755) < 0) {
		die("cannot create directory /writable");
	}

	if (umount_all()) {
		kmsg("- found no hard-to-unmount writable partition.");
	} else {
		if (mount("/oldroot/writable", "/writable", NULL, MS_MOVE, NULL)
		    < 0) {
			die("cannot move writable out of the way");
		}

		bool ok = umount_all();
		kmsg("%c was %s to unmount writable cleanly", ok ? '-' : '*',
		     ok ? "able" : "*NOT* able");
		sync();		// shouldn't be needed, but just in case
	}

	// argv[1] can be one of at least: halt, reboot, poweroff.
	// FIXME: might also be kexec, hibernate or hybrid-sleep -- support those!

	int cmd = RB_HALT_SYSTEM;

	if (argc < 2) {
		kmsg("* called without verb; halting.");
	} else {
		if (sc_streq("reboot", argv[1])) {
			cmd = RB_AUTOBOOT;
			kmsg("- rebooting.");
		} else if (sc_streq("poweroff", argv[1])) {
			cmd = RB_POWER_OFF;
			kmsg("- powering off.");
		} else if (sc_streq("halt", argv[1])) {
			kmsg("- halting.");
		} else {
			kmsg("* called with unsupported verb %s; halting.",
			     argv[1]);
		}
	}

	reboot(cmd);

	return 0;
}
