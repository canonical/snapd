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
#ifdef ROOTFS_IS_CORE_SNAP
#include <sys/syscall.h>
#endif
#include <errno.h>
#include <sched.h>
#include <string.h>
#include <mntent.h>

#include "utils.h"
#include "snap.h"
#include "classic.h"
#include "mount-support-nvidia.h"

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

	// chdir to '/' since the mount won't apply to the current directory
	char *pwd = get_current_dir_name();
	if (pwd == NULL)
		die("unable to get current directory");
	if (chdir("/") != 0)
		die("unable to change directory to '/'");

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
	// chdir to original directory
	if (chdir(pwd) != 0)
		die("unable to change to original directory");
	free(pwd);

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

#ifdef NVIDIA_ARCH
static void sc_bind_mount_hostfs(const char *rootfs_dir)
{
	// Create a read-only bind mount from "/" to
	// "$rootfs_dir/var/lib/snapd/hostfs".
	char buf[512];
	must_snprintf(buf, sizeof buf, "%s%s", rootfs_dir, SC_HOSTFS_DIR);
	debug("bind-mounting host filesystem at %s", buf);
	if (mount("/", buf, NULL, MS_BIND | MS_RDONLY, NULL) != 0) {
		if (errno == ENOENT) {
			die("cannot bind-mount host filesystem\n"
			    "the core snap is too old, please run: snap refresh ubuntu-core");
		} else {
			die("cannot bind-mount host filesystem at %s", buf);
		}
	}
}
#endif				// ifdef NVIDIA_ARCH

void setup_snappy_os_mounts()
{
	debug("%s", __func__);
#ifdef ROOTFS_IS_CORE_SNAP
	char rootfs_dir[MAX_BUF] = { 0 };
	// Create a temporary directory that will become the root directory of this
	// process later on. The directory will be used as a mount point for the
	// core snap.
	//
	// XXX: This directory is never cleaned up today.
	must_snprintf(rootfs_dir, sizeof(rootfs_dir),
		      "/tmp/snap.rootfs_XXXXXX");
	if (mkdtemp(rootfs_dir) == NULL) {
		die("cannot create temporary directory for the root file system");
	}
	// Bind mount the OS snap into the rootfs directory.
	const char *core_snap_dir = "/snap/ubuntu-core/current";
	debug("bind mounting core snap: %s -> %s", core_snap_dir, rootfs_dir);
	if (mount(core_snap_dir, rootfs_dir, NULL, MS_BIND, NULL) != 0) {
		die("cannot bind mount core snap: %s to %s", core_snap_dir,
		    rootfs_dir);
	}
	// Bind mount certain directories from the rootfs directory (with the core
	// snap) to various places on the host OS. Each directory is justified with
	// a short comment below.
	const char *source_mounts[] = {
		"/dev",		// because it contains devices on host OS
		"/etc",		// because that's where /etc/resolv.conf lives, perhaps a bad idea
		"/home",	// to support /home/*/snap and home interface
		"/proc",	// fundamental filesystem
		"/snap",	// to get access to all the snaps
		"/sys",		// fundamental filesystem
		"/tmp",		// to get writable tmp
		"/var/snap",	// to get access to global snap data
		"/var/lib/snapd",	// to get access to snapd state and seccomp profiles
		"/var/tmp",	// to get access to the other temporary directory
	};
	for (int i = 0; i < sizeof(source_mounts) / sizeof *source_mounts; i++) {
		const char *src = source_mounts[i];
		char dst[512];
		must_snprintf(dst, sizeof dst, "%s%s", rootfs_dir,
			      source_mounts[i]);
		debug("bind mounting %s to %s", src, dst);
		// NOTE: MS_REC so that we can see anything that may be mounted under
		// any of the directories already. This is crucial for /snap, for
		// example.
		//
		// NOTE: MS_SLAVE so that the started process cannot maliciously mount
		// anything into those places and affect the system on the outside.
		if (mount(src, dst, NULL, MS_BIND | MS_REC | MS_SLAVE, NULL) !=
		    0) {
			die("cannot bind mount %s to %s", src, dst);
		}
	}
#ifdef NVIDIA_ARCH
	// Make this conditional on Nvidia support for Arch as Ubuntu doesn't use
	// this so far and it requires a very recent version of the core snap.
	sc_bind_mount_hostfs(rootfs_dir);
#endif
	sc_mount_nvidia_driver(rootfs_dir);
	// Chroot into the new root filesystem so that / is the core snap.  Why are
	// we using something as esoteric as pivot_root? Because this makes apparmor
	// handling easy. Using a normal chroot makes all apparmor rules conditional.
	// We are either running on an all-snap system where this would-be chroot
	// didn't happen and all the rules see / as the root file system _OR_
	// we are running on top of a classic distribution and this chroot has now
	// moved all paths to /tmp/snap.rootfs_*. Because we are using unshare with
	// CLONE_NEWNS we can essentially use pivot_root just like chroot but this
	// makes apparmor unaware of the old root so everything works okay.
	debug("chrooting into %s", rootfs_dir);
	if (chdir(rootfs_dir) == -1) {
		die("cannot change working directory to %s", rootfs_dir);
	}
	if (syscall(SYS_pivot_root, ".", rootfs_dir) == -1) {
		die("cannot pivot_root to the new root filesystem");
	}
	// Reset path as we cannot rely on the path from the host OS to
	// make sense. The classic distribution may use any PATH that makes
	// sense but we cannot assume it makes sense for the core snap
	// layout. Note that the /usr/local directories are explicitly
	// left out as they are not part of the core snap.
	debug("resetting PATH to values in sync with core snap");
	setenv("PATH", "/usr/sbin:/usr/bin:/sbin:/bin:/usr/games", 1);
#else
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
	sc_mount_nvidia_driver("");
#endif				// ROOTFS_IS_CORE_SNAP
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

void setup_bind_mounts(const char *appname)
{
	debug("%s: %s", __FUNCTION__, appname);

	FILE *f = NULL;
	const char *bind_profile_dir = "/var/lib/snapd/bind/profiles/";

	char profile_path[PATH_MAX];
	int snprintf_rc =
	    snprintf(profile_path, sizeof(profile_path), "%s/%s.bind",
		     bind_profile_dir, appname);
	if (snprintf_rc < 0 || snprintf_rc >= 512) {
		errno = 0;
		die("snprintf returned unexpected value");
	}

	f = fopen(profile_path, "r");
	// it is ok for the file to not exist
	if (f == NULL && errno == ENOENT)
		return;
	// however any other error is a real error
	if (f == NULL) {
                fprintf(stderr, "cannot open %s\n", profile_path);
		die("aborting");
	}

	struct mntent *m = NULL;
	while ((m = getmntent(f)) != NULL) {
		int flags = MS_BIND;

		if (strcmp(m->mnt_type, "") != 0) {
			die("only bind mounts are supported");
		}
		if (hasmntopt(m, "bind") == NULL) {
			die("need bind mount flag");
		}
		if (hasmntopt(m, "ro") != NULL) {
			flags |= MS_RDONLY;
		}

		if (mount(m->mnt_fsname, m->mnt_dir, NULL, flags, NULL) != 0) {
			die("unable to bind private /tmp");
		}
	}

	if (f != NULL) {
		if (fclose(f) != 0)
			die("could not close bind mount file");
	}

	return;
}
