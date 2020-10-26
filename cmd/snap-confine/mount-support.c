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

#define _GNU_SOURCE

#ifdef HAVE_CONFIG_H
#include "config.h"
#endif

#include "mount-support.h"

#include <errno.h>
#include <fcntl.h>
#include <libgen.h>
#include <limits.h>
#include <mntent.h>
#include <sched.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

#include "../libsnap-confine-private/apparmor-support.h"
#include "../libsnap-confine-private/classic.h"
#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/mount-opt.h"
#include "../libsnap-confine-private/mountinfo.h"
#include "../libsnap-confine-private/snap.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/tool.h"
#include "../libsnap-confine-private/utils.h"
#include "../libsnap-confine-private/feature.h"
#include "mount-support-nvidia.h"

#define MAX_BUF 1000

static void sc_detach_views_of_writable(sc_distro distro, bool normal_mode);

// TODO: simplify this, after all it is just a tmpfs
// TODO: fold this into bootstrap
static void setup_private_mount(const char *snap_name)
{
	// Create a 0700 base directory. This is the "base" directory that is
	// protected from other users. This directory name is NOT randomly
	// generated. This has several properties:
	//
	// Users can relate to the name and can find the temporary directory as
	// visible from within the snap. If this directory was random it would be
	// harder to find because there may be situations in which multiple
	// directories related to the same snap name would exist.
	//
	// Snapd can partially manage the directory. Specifically on snap remove
	// snapd could remove the directory and everything in it, potentially
	// avoiding runaway disk use on a machine that either never reboots or uses
	// persistent /tmp directory.
	//
	// Underneath the base directory there is a "tmp" sub-directory that has
	// mode 1777 and behaves as a typical /tmp directory would. That directory
	// is used as a bind-mounted /tmp directory.
	//
	// Because the directories are reused across invocations by distinct users
	// and because the directories are trivially guessable, each invocation
	// unconditionally chowns/chmods them to appropriate values.
	char base_dir[MAX_BUF] = { 0 };
	char tmp_dir[MAX_BUF] = { 0 };
	int base_dir_fd SC_CLEANUP(sc_cleanup_close) = -1;
	int tmp_dir_fd SC_CLEANUP(sc_cleanup_close) = -1;
	sc_must_snprintf(base_dir, sizeof(base_dir), "/tmp/snap.%s", snap_name);
	sc_must_snprintf(tmp_dir, sizeof(tmp_dir), "%s/tmp", base_dir);

	/* Switch to root group so that mkdir and open calls below create filesystem
	 * elements that are not owned by the user calling into snap-confine. */
	sc_identity old = sc_set_effective_identity(sc_root_group_identity());
	// Create /tmp/snap.$SNAP_NAME/ 0700 root.root. Ignore EEXIST since we want
	// to reuse and we will open with O_NOFOLLOW, below.
	if (mkdir(base_dir, 0700) < 0 && errno != EEXIST) {
		die("cannot create base directory %s", base_dir);
	}
	base_dir_fd = open(base_dir,
			   O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
	if (base_dir_fd < 0) {
		die("cannot open base directory %s", base_dir);
	}
	/* This seems redundant on first read but it has the non-obvious
	 * property of changing existing directories  that have already existed
	 * but had incorrect ownership or permission. This is possible due to
	 * earlier bugs in snap-confine and due to the fact that some systems
	 * use persistent /tmp directory and may not clean up leftover files
	 * for arbitrarily long. This comment applies the following two pairs
	 * of fchmod and fchown. */
	if (fchmod(base_dir_fd, 0700) < 0) {
		die("cannot chmod base directory %s to 0700", base_dir);
	}
	if (fchown(base_dir_fd, 0, 0) < 0) {
		die("cannot chown base directory %s to root.root", base_dir);
	}
	// Create /tmp/snap.$SNAP_NAME/tmp 01777 root.root Ignore EEXIST since we
	// want to reuse and we will open with O_NOFOLLOW, below.
	if (mkdirat(base_dir_fd, "tmp", 01777) < 0 && errno != EEXIST) {
		die("cannot create private tmp directory %s/tmp", base_dir);
	}
	(void)sc_set_effective_identity(old);
	tmp_dir_fd = openat(base_dir_fd, "tmp",
			    O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
	if (tmp_dir_fd < 0) {
		die("cannot open private tmp directory %s/tmp", base_dir);
	}
	if (fchmod(tmp_dir_fd, 01777) < 0) {
		die("cannot chmod private tmp directory %s/tmp to 01777",
		    base_dir);
	}
	if (fchown(tmp_dir_fd, 0, 0) < 0) {
		die("cannot chown private tmp directory %s/tmp to root.root",
		    base_dir);
	}
	sc_do_mount(tmp_dir, "/tmp", NULL, MS_BIND, NULL);
	sc_do_mount("none", "/tmp", NULL, MS_PRIVATE, NULL);
}

// TODO: fold this into bootstrap
static void setup_private_pts(void)
{
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
		die("cannot stat /dev/pts/ptmx");
	}
	// Make sure /dev/ptmx exists so we can bind mount over it
	if (stat("/dev/ptmx", &st) != 0) {
		die("cannot stat /dev/ptmx");
	}
	// Since multi-instance, use ptmxmode=0666. The other options are
	// copied from /etc/default/devpts
	sc_do_mount("devpts", "/dev/pts", "devpts", MS_MGC_VAL,
		    "newinstance,ptmxmode=0666,mode=0620,gid=5");
	sc_do_mount("/dev/pts/ptmx", "/dev/ptmx", "none", MS_BIND, 0);
}

struct sc_mount {
	const char *path;
	bool is_bidirectional;
	// Alternate path defines the rbind mount "alternative" of path.
	// It exists so that we can make /media on systems that use /run/media.
	const char *altpath;
	// Optional mount points are not processed unless the source and
	// destination both exist.
	bool is_optional;
};

struct sc_mount_config {
	const char *rootfs_dir;
	// The struct is terminated with an entry with NULL path.
	const struct sc_mount *mounts;
	sc_distro distro;
	bool normal_mode;
	const char *base_snap_name;
};

/**
 * Bootstrap mount namespace.
 *
 * This is a chunk of tricky code that lets us have full control over the
 * layout and direction of propagation of mount events. The documentation below
 * assumes knowledge of the 'sharedsubtree.txt' document from the kernel source
 * tree.
 *
 * As a reminder two definitions are quoted below:
 *
 *  A 'propagation event' is defined as event generated on a vfsmount
 *  that leads to mount or unmount actions in other vfsmounts.
 *
 *  A 'peer group' is defined as a group of vfsmounts that propagate
 *  events to each other.
 *
 * (end of quote).
 *
 * The main idea is to setup a mount namespace that has a root filesystem with
 * vfsmounts and peer groups that, depending on the location, either isolate
 * or share with the rest of the system.
 *
 * The vast majority of the filesystem is shared in one direction. Events from
 * the outside (from the main mount namespace) propagate inside (to namespaces
 * of particular snaps) so things like new snap revisions, mounted drives, etc,
 * just show up as expected but even if a snap is exploited or malicious in
 * nature it cannot affect anything in another namespace where it might cause
 * security or stability issues.
 *
 * Selected directories (today just /media) can be shared in both directions.
 * This allows snaps with sufficient privileges to either create, through the
 * mount system call, additional mount points that are visible by the rest of
 * the system (both the main mount namespace and namespaces of individual
 * snaps) or remove them, through the unmount system call.
 **/
static void sc_bootstrap_mount_namespace(const struct sc_mount_config *config)
{
	char scratch_dir[] = "/tmp/snap.rootfs_XXXXXX";
	char src[PATH_MAX] = { 0 };
	char dst[PATH_MAX] = { 0 };
	if (mkdtemp(scratch_dir) == NULL) {
		die("cannot create temporary directory for the root file system");
	}
	// NOTE: at this stage we just called unshare(CLONE_NEWNS). We are in a new
	// mount namespace and have a private list of mounts.
	debug("scratch directory for constructing namespace: %s", scratch_dir);
	// Make the root filesystem recursively shared. This way propagation events
	// will be shared with main mount namespace.
	sc_do_mount("none", "/", NULL, MS_REC | MS_SHARED, NULL);
	// Bind mount the temporary scratch directory for root filesystem over
	// itself so that it is a mount point. This is done so that it can become
	// unbindable as explained below.
	sc_do_mount(scratch_dir, scratch_dir, NULL, MS_BIND, NULL);
	// Make the scratch directory unbindable.
	//
	// This is necessary as otherwise a mount loop can occur and the kernel
	// would crash. The term unbindable simply states that it cannot be bind
	// mounted anywhere. When we construct recursive bind mounts below this
	// guarantees that this directory will not be replicated anywhere.
	sc_do_mount("none", scratch_dir, NULL, MS_UNBINDABLE, NULL);
	// Recursively bind mount desired root filesystem directory over the
	// scratch directory. This puts the initial content into the scratch space
	// and serves as a foundation for all subsequent operations below.
	//
	// The mount is recursive because it can either be applied to the root
	// filesystem of a core system (aka all-snap) or the core snap on a classic
	// system. In the former case we need recursive bind mounts to accurately
	// replicate the state of the root filesystem into the scratch directory.
	sc_do_mount(config->rootfs_dir, scratch_dir, NULL, MS_REC | MS_BIND,
		    NULL);
	// Make the scratch directory recursively slave. Nothing done there will be
	// shared with the initial mount namespace. This effectively detaches us,
	// in one way, from the original namespace and coupled with pivot_root
	// below serves as the foundation of the mount sandbox.
	sc_do_mount("none", scratch_dir, NULL, MS_REC | MS_SLAVE, NULL);
	// Bind mount certain directories from the host filesystem to the scratch
	// directory. By default mount events will propagate in both into and out
	// of the peer group. This way the running application can alter any global
	// state visible on the host and in other snaps. This can be restricted by
	// disabling the "is_bidirectional" flag as can be seen below.
	for (const struct sc_mount * mnt = config->mounts; mnt->path != NULL;
	     mnt++) {

		if (mnt->is_bidirectional) {
			sc_identity old =
			    sc_set_effective_identity(sc_root_group_identity());
			if (mkdir(mnt->path, 0755) < 0 && errno != EEXIST) {
				die("cannot create %s", mnt->path);
			}
			(void)sc_set_effective_identity(old);
		}
		sc_must_snprintf(dst, sizeof dst, "%s/%s", scratch_dir,
				 mnt->path);
		if (mnt->is_optional) {
			bool ok = sc_do_optional_mount(mnt->path, dst, NULL,
						       MS_REC | MS_BIND, NULL);
			if (!ok) {
				// If we cannot mount it, just continue.
				continue;
			}
		} else {
			sc_do_mount(mnt->path, dst, NULL, MS_REC | MS_BIND,
				    NULL);
		}
		if (!mnt->is_bidirectional) {
			// Mount events will only propagate inwards to the namespace. This
			// way the running application cannot alter any global state apart
			// from that of its own snap.
			sc_do_mount("none", dst, NULL, MS_REC | MS_SLAVE, NULL);
		}
		if (mnt->altpath == NULL) {
			continue;
		}
		// An alternate path of mnt->path is provided at another location.
		// It should behave exactly the same as the original.
		sc_must_snprintf(dst, sizeof dst, "%s/%s", scratch_dir,
				 mnt->altpath);
		struct stat stat_buf;
		if (lstat(dst, &stat_buf) < 0) {
			die("cannot lstat %s", dst);
		}
		if ((stat_buf.st_mode & S_IFMT) == S_IFLNK) {
			die("cannot bind mount alternate path over a symlink: %s", dst);
		}
		sc_do_mount(mnt->path, dst, NULL, MS_REC | MS_BIND, NULL);
		if (!mnt->is_bidirectional) {
			sc_do_mount("none", dst, NULL, MS_REC | MS_SLAVE, NULL);
		}
	}
	if (config->normal_mode) {
		// Since we mounted /etc from the host filesystem to the scratch directory,
		// we may need to put certain directories from the desired root filesystem
		// (e.g. the core snap) back. This way the behavior of running snaps is not
		// affected by the alternatives directory from the host, if one exists.
		//
		// Fixes the following bugs:
		//  - https://bugs.launchpad.net/snap-confine/+bug/1580018
		//  - https://bugzilla.opensuse.org/show_bug.cgi?id=1028568
		const char *dirs_from_core[] = {
			"/etc/alternatives", "/etc/ssl", "/etc/nsswitch.conf",
			// Some specifc and privileged interfaces (e.g docker-support) give
			// access to apparmor_parser from the base snap which at a minimum
			// needs to use matching configuration from the base snap instead
			// of from the users host system.
			"/etc/apparmor", "/etc/apparmor.d",
			NULL
		};
		for (const char **dirs = dirs_from_core; *dirs != NULL; dirs++) {
			const char *dir = *dirs;
			if (access(dir, F_OK) != 0) {
				continue;
			}
			struct stat dst_stat;
			struct stat src_stat;
			sc_must_snprintf(src, sizeof src, "%s%s",
					 config->rootfs_dir, dir);
			sc_must_snprintf(dst, sizeof dst, "%s%s",
					 scratch_dir, dir);
			if (lstat(src, &src_stat) != 0) {
				if (errno == ENOENT) {
					continue;
				}
				die("cannot stat %s from desired rootfs", src);
			}
			if (!S_ISREG(src_stat.st_mode)
			    && !S_ISDIR(src_stat.st_mode)) {
				debug
				    ("entry %s from the desired rootfs is not a file or directory, skipping mount",
				     src);
				continue;
			}

			if (lstat(dst, &dst_stat) != 0) {
				if (errno == ENOENT) {
					continue;
				}
				die("cannot stat %s from host", src);
			}
			if (!S_ISREG(dst_stat.st_mode)
			    && !S_ISDIR(dst_stat.st_mode)) {
				debug
				    ("entry %s from the host is not a file or directory, skipping mount",
				     src);
				continue;
			}

			if ((dst_stat.st_mode & S_IFMT) !=
			    (src_stat.st_mode & S_IFMT)) {
				debug
				    ("entries %s and %s are of different types, skipping mount",
				     dst, src);
				continue;
			}
			// both source and destination exist where both are either files
			// or both are directories
			sc_do_mount(src, dst, NULL, MS_BIND, NULL);
			sc_do_mount("none", dst, NULL, MS_SLAVE, NULL);
		}
	}
	// Provide /usr/lib/snapd with essential snapd tools. There are two methods
	// for doing this. The more recent method involves setting up symlink
	// transpolines pointing to tools exported by snapd from either snapd snap,
	// the core snap or from the classic host. This method allows tools to
	// change as snapd snap refreshes and is used by default. The older method
	// either uses tools embedded in the core snap, if used as base, or provides
	// a one-time snapshot of snapd tools, matching the revision used when the
	// first snap process is started.
	//
	// The first method is preferred but to cope with for unforeseen problems
	// the second method can be used by explicitly disable the feature flag
	// referenced below.
	if (sc_feature_enabled(SC_FEATURE_USE_EXPORTED_SNAPD_TOOLS)) {
		// Open the /usr/lib/snapd inside the scratch space and mount a tmpfs
		// there. The use of MS_NOEXEC is safe, as we only place symbolic links
		// to executables and never execute anything placed there directly.
		sc_must_snprintf(dst, sizeof dst, "%s/usr/lib/snapd",
				 scratch_dir);
		sc_do_mount("none", dst, "tmpfs", MS_NODEV | MS_NOEXEC,
			    "mode=755");
		int tools_dir_fd SC_CLEANUP(sc_cleanup_close) = -1;
		// XXX: O_PATH is unavailable despite the rigth headers and feature flags.
		// Needs debugging but perhaps not now.
		tools_dir_fd =
		    open(dst, O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW | __O_PATH);
		if (tools_dir_fd < 0) {
			die("cannot open %s", dst);
		}
		// Create synlinks to all the snap tool files exported by snapd.
		static const char *const tools[] = {
			"etelpmoc.sh",
			"info",
			"snap-confine",
			"snap-discard-ns",
			"snap-exec",
			"snap-gdb-shim",
			"snap-gdbserver-shim",
			"snap-update-ns",
			"snapctl",
			NULL,
		};
		for (const char *const *tool = tools; *tool != NULL; ++tool) {
			char symlink_target[PATH_MAX + 1] = { 0 };
			sc_must_snprintf(symlink_target, sizeof symlink_target,
					 "/var/lib/snapd/export/snapd/current/tools/%s",
					 *tool);
			if (symlinkat(symlink_target, tools_dir_fd, *tool) < 0) {
				die("cannot link to %s", *tool);
			}
		}

		// Prevent modification by most snaps. Alter the mount point rather than
		// the file system as LXD prevents us from mounting the entire file
		// system read only.
		sc_do_mount("none", dst, NULL, MS_REMOUNT | MS_BIND | MS_RDONLY,
			    NULL);
		sc_do_mount("none", dst, NULL, MS_SLAVE, NULL);
	} else {
		// The "core" base snap is special as it contains snapd and friends.
		// Other base snaps do not, so whenever a base snap other than core is
		// in use we need extra provisions for setting up internal tooling to
		// be available.
		if (config->distro == SC_DISTRO_CORE_OTHER
		    || !sc_streq(config->base_snap_name, "core")) {
			sc_must_snprintf(dst, sizeof dst, "%s/usr/lib/snapd",
					 scratch_dir);

			// bind mount the current $ROOT/usr/lib/snapd path,
			// where $ROOT is either "/" or the "/snap/{core,snapd}/current"
			// that we are re-execing from
			char *src = NULL;
			char self[PATH_MAX + 1] = { 0 };
			ssize_t nread;
			nread =
			    readlink("/proc/self/exe", self, sizeof self - 1);
			if (nread < 0) {
				die("cannot read /proc/self/exe");
			}
			// Though we initialized self to NULs and passed one less to
			// readlink, therefore guaranteeing that self is
			// zero-terminated, perform an explicit assignment to make
			// Coverity happy.
			self[nread] = '\0';
			// this cannot happen except when the kernel is buggy
			if (strstr(self, "/snap-confine") == NULL) {
				die("cannot use result from readlink: %s",
				    self);
			}
			src = dirname(self);
			// dirname(path) might return '.' depending on path.
			// /proc/self/exe should always point
			// to an absolute path, but let's guarantee that.
			if (src[0] != '/') {
				die("cannot use the result of dirname(): %s",
				    src);
			}
			sc_do_mount(src, dst, NULL, MS_BIND | MS_RDONLY, NULL);
			sc_do_mount("none", dst, NULL, MS_SLAVE, NULL);
		}
	}
	// Bind mount the directory where all snaps are mounted. The location of
	// the this directory on the host filesystem may not match the location in
	// the desired root filesystem. In the "core" and "ubuntu-core" snaps the
	// directory is always /snap. On the host it is a build-time configuration
	// option stored in SNAP_MOUNT_DIR. In legacy mode (or in other words, not
	// in normal mode), we don't need to do this because /snap is fixed and
	// already contains the correct view of the mounted snaps.
	if (config->normal_mode) {
		sc_must_snprintf(dst, sizeof dst, "%s/snap", scratch_dir);
		sc_do_mount(SNAP_MOUNT_DIR, dst, NULL, MS_BIND | MS_REC, NULL);
		sc_do_mount("none", dst, NULL, MS_REC | MS_SLAVE, NULL);
	}
	// Create the hostfs directory if one is missing. This directory is a part
	// of packaging now so perhaps this code can be removed later.
	sc_identity old = sc_set_effective_identity(sc_root_group_identity());
	if (mkdir(SC_HOSTFS_DIR, 0755) < 0) {
		if (errno != EEXIST) {
			die("cannot perform operation: mkdir %s", SC_HOSTFS_DIR);
		}
	}
	(void)sc_set_effective_identity(old);
	// Ensure that hostfs isgroup owned by root. We may have (now or earlier)
	// created the directory as the user who first ran a snap on a given
	// system and the group identity of that user is visible on disk.
	// This was LP:#1665004
	struct stat sb;
	if (stat(SC_HOSTFS_DIR, &sb) < 0) {
		die("cannot stat %s", SC_HOSTFS_DIR);
	}
	if (sb.st_uid != 0 || sb.st_gid != 0) {
		if (chown(SC_HOSTFS_DIR, 0, 0) < 0) {
			die("cannot change user/group owner of %s to root",
			    SC_HOSTFS_DIR);
		}
	}
	// Make the upcoming "put_old" directory for pivot_root private so that
	// mount events don't propagate to any peer group. In practice pivot root
	// has a number of undocumented requirements and one of them is that the
	// "put_old" directory (the second argument) cannot be shared in any way.
	sc_must_snprintf(dst, sizeof dst, "%s/%s", scratch_dir, SC_HOSTFS_DIR);
	sc_do_mount(dst, dst, NULL, MS_BIND, NULL);
	sc_do_mount("none", dst, NULL, MS_PRIVATE, NULL);
	// On classic mount the nvidia driver. Ideally this would be done in an
	// uniform way after pivot_root but this is good enough and requires less
	// code changes the nvidia code assumes it has access to the existing
	// pre-pivot filesystem.
	if (config->distro == SC_DISTRO_CLASSIC) {
		sc_mount_nvidia_driver(scratch_dir);
	}
	// XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
	//                    pivot_root
	// XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
	// Use pivot_root to "chroot" into the scratch directory.
	//
	// Q: Why are we using something as esoteric as pivot_root(2)?
	// A: Because this makes apparmor handling easy. Using a normal chroot
	// makes all apparmor rules conditional.  We are either running on an
	// all-snap system where this would-be chroot didn't happen and all the
	// rules see / as the root file system _OR_ we are running on top of a
	// classic distribution and this chroot has now moved all paths to
	// /tmp/snap.rootfs_*.
	//
	// Because we are using unshare(2) with CLONE_NEWNS we can essentially use
	// pivot_root just like chroot but this makes apparmor unaware of the old
	// root so everything works okay.
	//
	// HINT: If you are debugging this and are trying to see why pivot_root
	// happens to return EINVAL with any changes you may be making, please
	// consider applying
	// misc/0001-Add-printk-based-debugging-to-pivot_root.patch to your tree
	// kernel.
	debug("performing operation: pivot_root %s %s", scratch_dir, dst);
	if (syscall(SYS_pivot_root, scratch_dir, dst) < 0) {
		die("cannot perform operation: pivot_root %s %s", scratch_dir,
		    dst);
	}
	// Unmount the self-bind mount over the scratch directory created earlier
	// in the original root filesystem (which is now mounted on SC_HOSTFS_DIR).
	// This way we can remove the temporary directory we created and "clean up"
	// after ourselves nicely.
	sc_must_snprintf(dst, sizeof dst, "%s/%s", SC_HOSTFS_DIR, scratch_dir);
	sc_do_umount(dst, UMOUNT_NOFOLLOW);
	// Remove the scratch directory. Note that we are using the path that is
	// based on the old root filesystem as after pivot_root we cannot guarantee
	// what is present at the same location normally. (It is probably an empty
	// /tmp directory that is populated in another place).
	debug("performing operation: rmdir %s", dst);
	if (rmdir(scratch_dir) < 0) {
		die("cannot perform operation: rmdir %s", dst);
	};
	// Make the old root filesystem recursively slave. This way operations
	// performed in this mount namespace will not propagate to the peer group.
	// This is another essential part of the confinement system.
	sc_do_mount("none", SC_HOSTFS_DIR, NULL, MS_REC | MS_SLAVE, NULL);
	// Detach the redundant hostfs version of sysfs since it shows up in the
	// mount table and software inspecting the mount table may become confused
	// (eg, docker and LP:# 162601).
	sc_must_snprintf(src, sizeof src, "%s/sys", SC_HOSTFS_DIR);
	sc_do_umount(src, UMOUNT_NOFOLLOW | MNT_DETACH);
	// Detach the redundant hostfs version of /dev since it shows up in the
	// mount table and software inspecting the mount table may become confused.
	sc_must_snprintf(src, sizeof src, "%s/dev", SC_HOSTFS_DIR);
	sc_do_umount(src, UMOUNT_NOFOLLOW | MNT_DETACH);
	// Detach the redundant hostfs version of /proc since it shows up in the
	// mount table and software inspecting the mount table may become confused.
	sc_must_snprintf(src, sizeof src, "%s/proc", SC_HOSTFS_DIR);
	sc_do_umount(src, UMOUNT_NOFOLLOW | MNT_DETACH);
	// Detach both views of /writable: the one from hostfs and the one directly
	// visible in /writable. Interfaces don't grant access to this directory
	// and it has a large duplicated view of many mount points.  Note that this
	// is only applicable to ubuntu-core systems.
	sc_detach_views_of_writable(config->distro, config->normal_mode);
}

static void sc_detach_views_of_writable(sc_distro distro, bool normal_mode)
{
	// Note that prior to detaching either mount point we switch the
	// propagation to private to both limit the change to just this view and to
	// prevent otherwise occurring event propagation from self-conflicting and
	// returning EBUSY. A similar approach is used by snap-update-ns and is
	// documented in umount(2).
	const char *writable_dir = "/writable";
	const char *hostfs_writable_dir = "/var/lib/snapd/hostfs/writable";

	// Writable only exists on ubuntu-core.
	if (distro == SC_DISTRO_CLASSIC) {
		return;
	}
	// On all core distributions we see /var/lib/snapd/hostfs/writable that
	// exposes writable, with a structure specific to ubuntu-core.
	debug("detaching %s", hostfs_writable_dir);
	sc_do_mount("none", hostfs_writable_dir, NULL,
		    MS_REC | MS_PRIVATE, NULL);
	sc_do_umount(hostfs_writable_dir, UMOUNT_NOFOLLOW | MNT_DETACH);

	// On ubuntu-core 16, when the executed snap uses core as base we also see
	// the /writable that we directly inherited from the initial mount
	// namespace.
	if (distro == SC_DISTRO_CORE16 && !normal_mode) {
		debug("detaching %s", writable_dir);
		sc_do_mount("none", writable_dir, NULL, MS_REC | MS_PRIVATE,
			    NULL);
		sc_do_umount(writable_dir, UMOUNT_NOFOLLOW | MNT_DETACH);
	}
}

/**
 * @path:    a pathname where / replaced with '\0'.
 * @offsetp: pointer to int showing which path segment was last seen.
 *           Updated on return to reflect the next segment.
 * @fulllen: full original path length.
 * Returns a pointer to the next path segment, or NULL if done.
 */
static char * __attribute__((used))
    get_nextpath(char *path, size_t *offsetp, size_t fulllen)
{
	size_t offset = *offsetp;

	if (offset >= fulllen)
		return NULL;

	while (offset < fulllen && path[offset] != '\0')
		offset++;
	while (offset < fulllen && path[offset] == '\0')
		offset++;

	*offsetp = offset;
	return (offset < fulllen) ? &path[offset] : NULL;
}

/**
 * Check that @subdir is a subdir of @dir.
**/
static bool __attribute__((used))
    is_subdir(const char *subdir, const char *dir)
{
	size_t dirlen = strlen(dir);
	size_t subdirlen = strlen(subdir);

	// @dir has to be at least as long as @subdir
	if (subdirlen < dirlen)
		return false;
	// @dir has to be a prefix of @subdir
	if (strncmp(subdir, dir, dirlen) != 0)
		return false;
	// @dir can look like "path/" (that is, end with the directory separator).
	// When that is the case then given the test above we can be sure @subdir
	// is a real subdirectory.
	if (dirlen > 0 && dir[dirlen - 1] == '/')
		return true;
	// @subdir can look like "path/stuff" and when the directory separator
	// is exactly at the spot where @dir ends (that is, it was not caught
	// by the test above) then @subdir is a real subdirectory.
	if (subdir[dirlen] == '/' && dirlen > 0)
		return true;
	// If both @dir and @subdir have identical length then given that the
	// prefix check above @subdir is a real subdirectory.
	if (subdirlen == dirlen)
		return true;
	return false;
}

void sc_populate_mount_ns(struct sc_apparmor *apparmor, int snap_update_ns_fd,
			  const sc_invocation * inv, const gid_t real_gid,
			  const gid_t saved_gid)
{
	// Classify the current distribution, as claimed by /etc/os-release.
	sc_distro distro = sc_classify_distro();

	// Check which mode we should run in, normal or legacy.
	if (inv->is_normal_mode) {
		// In normal mode we use the base snap as / and set up several bind mounts.
		const struct sc_mount mounts[] = {
			{"/dev"},	// because it contains devices on host OS
			{"/etc"},	// because that's where /etc/resolv.conf lives, perhaps a bad idea
			{"/home"},	// to support /home/*/snap and home interface
			{"/root"},	// because that is $HOME for services
			{"/proc"},	// fundamental filesystem
			{"/sys"},	// fundamental filesystem
			{"/tmp"},	// to get writable tmp
			{"/var/snap"},	// to get access to global snap data
			{"/var/lib/snapd"},	// to get access to snapd state and seccomp profiles
			{"/var/tmp"},	// to get access to the other temporary directory
			{"/run"},	// to get /run with sockets and what not
			{"/lib/modules",.is_optional = true},	// access to the modules of the running kernel
			{"/lib/firmware",.is_optional = true},	// access to the firmware of the running kernel
			{"/usr/src"},	// FIXME: move to SecurityMounts in system-trace interface
			{"/var/log"},	// FIXME: move to SecurityMounts in log-observe interface
#ifdef MERGED_USR
			{"/run/media", true, "/media"},	// access to the users removable devices
#else
			{"/media", true},	// access to the users removable devices
#endif				// MERGED_USR
			{"/run/netns", true},	// access to the 'ip netns' network namespaces
			// The /mnt directory is optional in base snaps to ensure backwards
			// compatibility with the first version of base snaps that was
			// released.
			{"/mnt",.is_optional = true},	// to support the removable-media interface
			{"/var/lib/extrausers",.is_optional = true},	// access to UID/GID of extrausers (if available)
			{},
		};
		struct sc_mount_config normal_config = {
			.rootfs_dir = inv->rootfs_dir,
			.mounts = mounts,
			.distro = distro,
			.normal_mode = true,
			.base_snap_name = inv->base_snap_name,
		};
		sc_bootstrap_mount_namespace(&normal_config);
	} else {
		// In legacy mode we don't pivot and instead just arrange bi-
		// directional mount propagation for two directories.
		const struct sc_mount mounts[] = {
			{"/media", true},
			{"/run/netns", true},
			{},
		};
		struct sc_mount_config legacy_config = {
			.rootfs_dir = "/",
			.mounts = mounts,
			.distro = distro,
			.normal_mode = false,
			.base_snap_name = inv->base_snap_name,
		};
		sc_bootstrap_mount_namespace(&legacy_config);
	}

	// TODO: rename this and fold it into bootstrap
	setup_private_mount(inv->snap_instance);
	// set up private /dev/pts
	// TODO: fold this into bootstrap
	setup_private_pts();

	// setup the security backend bind mounts
	sc_call_snap_update_ns(snap_update_ns_fd, inv->snap_instance, apparmor);
}

static bool is_mounted_with_shared_option(const char *dir)
    __attribute__((nonnull(1)));

static bool is_mounted_with_shared_option(const char *dir)
{
	sc_mountinfo *sm SC_CLEANUP(sc_cleanup_mountinfo) = NULL;
	sm = sc_parse_mountinfo(NULL);
	if (sm == NULL) {
		die("cannot parse /proc/self/mountinfo");
	}
	sc_mountinfo_entry *entry = sc_first_mountinfo_entry(sm);
	while (entry != NULL) {
		const char *mount_dir = entry->mount_dir;
		if (sc_streq(mount_dir, dir)) {
			const char *optional_fields = entry->optional_fields;
			if (strstr(optional_fields, "shared:") != NULL) {
				return true;
			}
		}
		entry = sc_next_mountinfo_entry(entry);
	}
	return false;
}

void sc_ensure_shared_snap_mount(void)
{
	if (!is_mounted_with_shared_option("/")
	    && !is_mounted_with_shared_option(SNAP_MOUNT_DIR)) {
		// TODO: We could be more aggressive and refuse to function but since
		// we have no data on actual environments that happen to limp along in
		// this configuration let's not do that yet.  This code should be
		// removed once we have a measurement and feedback mechanism that lets
		// us decide based on measurable data.
		sc_do_mount(SNAP_MOUNT_DIR, SNAP_MOUNT_DIR, "none",
			    MS_BIND | MS_REC, 0);
		sc_do_mount("none", SNAP_MOUNT_DIR, NULL, MS_SHARED | MS_REC,
			    NULL);
	}
}

void sc_setup_user_mounts(struct sc_apparmor *apparmor, int snap_update_ns_fd,
			  const char *snap_name)
{
	debug("%s: %s", __FUNCTION__, snap_name);

	char profile_path[PATH_MAX];
	struct stat st;

	sc_must_snprintf(profile_path, sizeof(profile_path),
			 "/var/lib/snapd/mount/snap.%s.user-fstab", snap_name);
	if (stat(profile_path, &st) != 0) {
		// It is ok for the user fstab to not exist.
		return;
	}

	// In our new mount namespace, recursively change all mounts
	// to slave mode, so we see changes from the parent namespace
	// but don't propagate our own changes.
	sc_do_mount("none", "/", NULL, MS_REC | MS_SLAVE, NULL);
	sc_identity old = sc_set_effective_identity(sc_root_group_identity());
	sc_call_snap_update_ns_as_user(snap_update_ns_fd, snap_name, apparmor);
	(void)sc_set_effective_identity(old);
}

void sc_ensure_snap_dir_shared_mounts(void)
{
	const char *dirs[] = { SNAP_MOUNT_DIR, "/var/snap", NULL };
	for (int i = 0; dirs[i] != NULL; i++) {
		const char *dir = dirs[i];
		if (!is_mounted_with_shared_option(dir)) {
			/* Since this directory isn't yet shared (but it should be),
			 * recursively bind mount it, then recursively share it so that
			 * changes to the host are seen in the snap and vice-versa. This
			 * allows us to fine-tune propagation events elsewhere for this new
			 * mountpoint.
			 *
			 * Not using MS_SLAVE because it's too late for SNAP_MOUNT_DIR,
			 * since snaps are already mounted, and it's not needed for
			 * /var/snap.
			 */
			sc_do_mount(dir, dir, "none", MS_BIND | MS_REC, 0);
			sc_do_mount("none", dir, NULL, MS_REC | MS_SHARED,
				    NULL);
		}
	}
}

void sc_setup_parallel_instance_classic_mounts(const char *snap_name,
					       const char *snap_instance_name)
{
	char src[PATH_MAX] = { 0 };
	char dst[PATH_MAX] = { 0 };

	const char *dirs[] = { SNAP_MOUNT_DIR, "/var/snap", NULL };
	for (int i = 0; dirs[i] != NULL; i++) {
		const char *dir = dirs[i];
		sc_do_mount("none", dir, NULL, MS_REC | MS_SLAVE, NULL);
	}

	/* Mount SNAP_MOUNT_DIR/<snap>_<key> on SNAP_MOUNT_DIR/<snap> */
	sc_must_snprintf(src, sizeof src, "%s/%s", SNAP_MOUNT_DIR,
			 snap_instance_name);
	sc_must_snprintf(dst, sizeof dst, "%s/%s", SNAP_MOUNT_DIR, snap_name);
	sc_do_mount(src, dst, "none", MS_BIND | MS_REC, 0);

	/* Mount /var/snap/<snap>_<key> on /var/snap/<snap> */
	sc_must_snprintf(src, sizeof src, "/var/snap/%s", snap_instance_name);
	sc_must_snprintf(dst, sizeof dst, "/var/snap/%s", snap_name);
	sc_do_mount(src, dst, "none", MS_BIND | MS_REC, 0);
}
