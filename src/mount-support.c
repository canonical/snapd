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
#include "mount-support.h"

#include <unistd.h>
#include <stdio.h>
#include <stdlib.h>
#include <limits.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <errno.h>
#include <sched.h>
#include <string.h>

#include "utils.h"
#include "snap.h"

#define MAX_BUF 1000

void setup_private_mount(const char *appname)
{
#ifdef STRICT_CONFINEMENT
	uid_t uid = getuid();
	gid_t gid = getgid();
	char tmpdir[MAX_BUF] = { 0 };

	// Create a 0700 base directory, this is the base dir that is
	// protected from other users.
	//
	// Under that basedir, we put a 1777 /tmp dir that is then bind
	// mounted for the applications to use
	must_snprintf(tmpdir, sizeof(tmpdir), "/tmp/snap.%d_%s_XXXXXX", uid,
		      appname);
	if (mkdtemp(tmpdir) == NULL) {
		die("unable to create tmpdir");
	}
	// now we create a 1777 /tmp inside our private dir
	mode_t old_mask = umask(0);
	char *d = strdup(tmpdir);
	if (!d) {
		die("Out of memory");
	}
	must_snprintf(tmpdir, sizeof(tmpdir), "%s/tmp", d);
	free(d);

	if (mkdir(tmpdir, 01777) != 0) {
		die("unable to create /tmp inside private dir");
	}
	umask(old_mask);

	// MS_BIND is there from linux 2.4
	if (mount(tmpdir, "/tmp", NULL, MS_BIND, NULL) != 0) {
		die("unable to bind private /tmp");
	}
	// MS_PRIVATE needs linux > 2.6.11
	if (mount("none", "/tmp", NULL, MS_PRIVATE, NULL) != 0) {
		die("unable to make /tmp/ private");
	}
	// do the chown after the bind mount to avoid potential shenanigans
	if (chown("/tmp/", uid, gid) < 0) {
		die("unable to chown tmpdir");
	}
	// ensure we set the various TMPDIRs to our newly created tmpdir
	const char *tmpd[] = { "TMPDIR", "TEMPDIR", NULL };
	int i;
	for (i = 0; tmpd[i] != NULL; i++) {
		if (setenv(tmpd[i], "/tmp", 1) != 0) {
			die("unable to set '%s'", tmpd[i]);
		}
	}
#endif				// ifdef STRICT_CONFINEMENT
}

void setup_private_pts()
{
#ifdef STRICT_CONFINEMENT
	// See https://www.kernel.org/doc/Documentation/filesystems/devpts.txt
	//
	// Ubuntu by default uses devpts 'single-instance' mode where
	// /dev/pts/ptmx is mounted with ptmxmode=0000. We don't want to change
	// the startup scripts though, so we follow the instructions in point
	// '4' of 'User-space changes' in the above doc. In other words, after
	// unshare(CLONE_NEWNS), we mount devpts with -o
	// newinstance,ptmxmode=0666 and then bind mount /dev/pts/ptmx onto
	// /dev/ptmx

	struct stat st;

	// Make sure /dev/pts/ptmx exists, otherwise we are in legacy mode
	// which doesn't provide the isolation we require.
	if (stat("/dev/pts/ptmx", &st) != 0) {
		die("/dev/pts/ptmx does not exist");
	}
	// Make sure /dev/ptmx exists so we can bind mount over it
	if (stat("/dev/ptmx", &st) != 0) {
		die("/dev/ptmx does not exist");
	}
	// Since multi-instance, use ptmxmode=0666. The other options are
	// copied from /etc/default/devpts
	if (mount("devpts", "/dev/pts", "devpts", MS_MGC_VAL,
		  "newinstance,ptmxmode=0666,mode=0620,gid=5")) {
		die("unable to mount a new instance of '/dev/pts'");
	}

	if (mount("/dev/pts/ptmx", "/dev/ptmx", "none", MS_BIND, 0)) {
		die("unable to mount '/dev/pts/ptmx'->'/dev/ptmx'");
	}
#endif				// ifdef STRICT_CONFINEMENT
}

void setup_snappy_os_mounts()
{
	debug("%s", __func__);

	// we mount some whitelisted directories
	//
	// Note that we do not mount "/etc/" from snappy. We could do that,
	// but if we do we need to ensure that data like /etc/{hostname,hosts,
	// passwd,groups} is in sync between the two systems (probably via
	// selected bind mounts of those files).
	const char *mounts[] =
	    { "/bin", "/sbin", "/lib", "/lib32", "/libx32", "/lib64", "/usr" };
	for (int i = 0; i < sizeof(mounts) / sizeof(char *); i++) {
		// we mount the OS snap /bin over the real /bin in this NS
		const char *dst = mounts[i];

		char buf[512];
		must_snprintf(buf, sizeof(buf), "/snap/ubuntu-core/current/%s",
			      dst);
		const char *src = buf;

		// some system do not have e.g. /lib64
		struct stat sbuf;
		if (stat(dst, &sbuf) != 0 || stat(src, &sbuf) != 0) {
			if (errno == ENOENT)
				continue;
			else
				die("could not stat mount point");
		}

		debug("mounting %s -> %s\n", src, dst);
		if (mount(src, dst, NULL, MS_BIND, NULL) != 0) {
			die("unable to bind %s to %s", src, dst);
		}
	}
}

void setup_slave_mount_namespace()
{
	// unshare() and CLONE_NEWNS require linux >= 2.6.16 and glibc >= 2.14
	// if using an older glibc, you'd need -D_BSD_SOURCE or -D_SVID_SORUCE.
	if (unshare(CLONE_NEWNS) < 0) {
		die("unable to set up mount namespace");
	}
	// make our "/" a rslave of the real "/". this means that
	// mounts from the host "/" get propagated to our namespace
	// (i.e. we see new media mounts)
	if (mount("none", "/", NULL, MS_REC | MS_SLAVE, NULL) != 0) {
		die("can not make make / rslave");
	}
}
