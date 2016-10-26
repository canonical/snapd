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
#include <string.h>		// strcmp, strncmp
#include <stdlib.h>		// exit
#include <stdio.h>		// fprintf, stderr
#include <sys/ioctl.h>		// ioctl
#include <linux/loop.h>		// LOOP_CLR_FD
#include <sys/reboot.h>		// reboot, RB_*
#include <fcntl.h>		// open
#include <errno.h>		// errno, sys_errlist

#include "mountinfo.h"

static bool streq(const char *a, const char *b)
{
	if (!a || !b) {
		return false;
	}

	size_t alen = strlen(a);
	size_t blen = strlen(b);

	if (alen != blen) {
		return false;
	}

	return strncmp(a, b, alen) == 0;
}

static bool endswith(const char *str, const char *suffix)
{
	if (!str || !suffix) {
		return false;
	}

	size_t xlen = strlen(suffix);
	size_t slen = strlen(str);

	if (slen < xlen) {
		return false;
	}

	return strncmp(str - xlen + slen, suffix, xlen) == 0;
}

__attribute__ ((format(printf, 1, 2)))
static void kmsg(const char *fmt, ...)
{
	static FILE *kmsg = NULL;
	static char *head = NULL;
	if (!kmsg) {
		// TODO: figure out why writing to /dev/kmsg doesn't work from here
		kmsg = stderr;
		head = "snapd system-shutdown helper: ";
	}

	va_list va;
	va_start(va, fmt);
	fputs(head, kmsg);
	vfprintf(kmsg, fmt, va);
	fprintf(kmsg, "\n");
	va_end(va);
}

__attribute__ ((noreturn))
static void die(const char *msg)
{
	if (errno == 0) {
		kmsg("*** %s", msg);
	} else {
		kmsg("*** %s: %s", msg, strerror(errno));
	}
	sync();
	reboot(RB_HALT_SYSTEM);
	exit(1);
}

static void detach_loop(const char *src)
{
	int fd = open(src, O_RDONLY);
	if (fd < 0) {
		kmsg("* unable to open loop device %s: %s", src,
		     strerror(errno));
	} else {
		if (ioctl(fd, LOOP_CLR_FD) < 0) {
			kmsg("* unable to disassociate loop device %ss: %s",
			     src, strerror(errno));
		}
		close(fd);
	}
}

// tries to umount all (well, most) things. Returns whether in the last pass it
// no longer found writable.
static bool umount_all()
{
	bool did_umount = true;
	bool had_writable = false;

	for (int i = 0; i < 10 && did_umount; i++) {
		struct mountinfo *mounts = parse_mountinfo(NULL);
		if (!mounts) {
			// oh dear
			die("unable to get mount info; giving up");
		}
		struct mountinfo_entry *cur = first_mountinfo_entry(mounts);

		had_writable = false;
		did_umount = false;
		while (cur) {
			const char *dir = mountinfo_entry_mount_dir(cur);
			const char *src = mountinfo_entry_mount_source(cur);
			unsigned major = mountinfo_entry_dev_major(cur);

			cur = next_mountinfo_entry(cur);

			if (streq("/", dir)) {
				continue;
			}

			if (streq("/dev", dir)) {
				continue;
			}

			if (streq("/proc", dir)) {
				continue;
			}

			if (major != 0 && major != 7
			    && endswith(dir, "/writable")) {
				had_writable = true;
			}

			if (umount(dir) == 0) {
				if (major == 7) {
					detach_loop(src);
				}

				did_umount = true;
			}
		}
		cleanup_mountinfo(&mounts);
	}

	return !had_writable;
}

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
	   as much as we can. Having done that we should be able to detah the
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
		if (strcmp("reboot", argv[1]) == 0) {
			cmd = RB_AUTOBOOT;
			kmsg("- rebooting.");
		} else if (strcmp("poweroff", argv[1]) == 0) {
			cmd = RB_POWER_OFF;
			kmsg("- powering off.");
		} else if (strcmp("halt", argv[1]) == 0) {
			kmsg("- halting.");
		} else {
			kmsg("* called with unsupported verb %s; halting.",
			     argv[1]);
		}
	}

	reboot(cmd);

	return 0;
}
