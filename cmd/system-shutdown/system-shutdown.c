/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
#include <string.h>		// strerror
#include <sys/ioctl.h>		// ioctl
#include <linux/loop.h>		// LOOP_CLR_FD
#include <sys/reboot.h>		// reboot, RB_*
#include <fcntl.h>		// open
#include <errno.h>		// errno, sys_errlist
#include <linux/reboot.h>	// LINUX_REBOOT_MAGIC*
#include <sys/syscall.h>	// SYS_reboot

#include "system-shutdown-utils.h"
#include "../libsnap-confine-private/string-utils.h"

int main(int argc, char *argv[])
{
	// 256 should be more than enough...
	char reboot_arg[256] = { 0 };

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

	/*
	   There are two¹ ways out of this program: we die, which calls sync
	   before halting the system; or we umount everything successfully
	   before doing whatever we were told to do, in which case there's
	   nothing left to sync.

	   1) ... apart from the third way that we never talk about: we somehow
	   are unable to umount everything cleanly, but go ahead with the
	   reboot anyway because no error was returned. That's the only path
	   we need to sync on explicitly.
	 */

	if (mkdir("/writable", 0755) < 0) {
		die("cannot create directory /writable");
	}
	// We are reading a file from /run and need to do this before unmounting
	if (sc_read_reboot_arg(reboot_arg, sizeof reboot_arg) < 0) {
		kmsg("no reboot parameter");
	}

	if (umount_all()) {
		kmsg("- found no hard-to-unmount writable partition.");
	} else {
		if (mount("/oldroot/writable", "/writable", NULL, MS_MOVE, NULL)
		    < 0) {
			die("cannot move writable out of the way");
		}

		if (umount_all()) {
			kmsg("- was able to unmount writable cleanly");
		} else {
			kmsg("* was *NOT* able to unmount writable cleanly");
			sync();	// we don't know what happened but we're going ahead
		}
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

	// glibc reboot wrapper does not expose the optional reboot syscall
	// parameter

	long ret;
	if (cmd == RB_AUTOBOOT && reboot_arg[0] != '\0') {
		ret = syscall(SYS_reboot,
			      LINUX_REBOOT_MAGIC1, LINUX_REBOOT_MAGIC2,
			      LINUX_REBOOT_CMD_RESTART2, reboot_arg);
	} else {
		ret = reboot(cmd);
	}

	if (ret == -1) {
		kmsg("cannot reboot the system: %s", strerror(errno));
	}

	return 0;
}
