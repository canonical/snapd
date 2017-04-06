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

#include <errno.h>
#include <fcntl.h>
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
#include <unistd.h>

#include "../libsnap-confine-private/classic.h"
#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/mount-opt.h"
#include "../libsnap-confine-private/mountinfo.h"
#include "../libsnap-confine-private/snap.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"
#include "mount-support-nvidia.h"
#include "quirks.h"

#define MAX_BUF 1000

/*!
 * The void directory.
 *
 * Snap confine moves to that directory in case it cannot retain the current
 * working directory across the pivot_root call.
 **/
#define SC_VOID_DIR "/var/lib/snapd/void"

/**
 * Get the path to the mounted core snap on the host distribution.
 *
 * The core snap may be named just "core" (preferred) or "ubuntu-core"
 * (legacy).  The mount point dependes on build-time configuration and may
 * differ from distribution to distribution.
 **/
static const char *sc_get_outer_core_mount_point()
{
	const char *core_path = SNAP_MOUNT_DIR "/core/current/";
	const char *ubuntu_core_path = SNAP_MOUNT_DIR "/ubuntu-core/current/";
	static const char *result = NULL;
	if (result == NULL) {
		if (access(core_path, F_OK) == 0) {
			// Use the "core" snap if available.
			result = core_path;
		} else if (access(ubuntu_core_path, F_OK) == 0) {
			// If not try to fall back to the "ubuntu-core" snap.
			result = ubuntu_core_path;
		} else {
			die("cannot locate the core snap");
		}
	}
	return result;
}

// TODO: simplify this, after all it is just a tmpfs
// TODO: fold this into bootstrap
static void setup_private_mount(const char *snap_name)
{
	uid_t uid = getuid();
	gid_t gid = getgid();
	char tmpdir[MAX_BUF] = { 0 };

	// Create a 0700 base directory, this is the base dir that is
	// protected from other users.
	//
	// Under that basedir, we put a 1777 /tmp dir that is then bind
	// mounted for the applications to use
	sc_must_snprintf(tmpdir, sizeof(tmpdir), "/tmp/snap.%d_%s_XXXXXX", uid,
			 snap_name);
	if (mkdtemp(tmpdir) == NULL) {
		die("cannot create temporary directory essential for private /tmp");
	}
	// now we create a 1777 /tmp inside our private dir
	mode_t old_mask = umask(0);
	char *d = strdup(tmpdir);
	if (!d) {
		die("cannot allocate memory for string copy");
	}
	sc_must_snprintf(tmpdir, sizeof(tmpdir), "%s/tmp", d);
	free(d);

	if (mkdir(tmpdir, 01777) != 0) {
		die("cannot create temporary directory for private /tmp");
	}
	umask(old_mask);

	// chdir to '/' since the mount won't apply to the current directory
	char *pwd = get_current_dir_name();
	if (pwd == NULL)
		die("cannot get current working directory");
	if (chdir("/") != 0)
		die("cannot change directory to '/'");

	// MS_BIND is there from linux 2.4
	sc_do_mount(tmpdir, "/tmp", NULL, MS_BIND, NULL);
	// MS_PRIVATE needs linux > 2.6.11
	sc_do_mount("none", "/tmp", NULL, MS_PRIVATE, NULL);
	// do the chown after the bind mount to avoid potential shenanigans
	if (chown("/tmp/", uid, gid) < 0) {
		die("cannot change ownership of /tmp");
	}
	// chdir to original directory
	if (chdir(pwd) != 0)
		die("cannot change current working directory to the original directory");
	free(pwd);

	// ensure we set the various TMPDIRs to our newly created tmpdir
	const char *tmpd[] = { "TMPDIR", "TEMPDIR", NULL };
	int i;
	for (i = 0; tmpd[i] != NULL; i++) {
		if (setenv(tmpd[i], "/tmp", 1) != 0) {
			die("cannot set environment variable '%s'", tmpd[i]);
		}
	}
}

// TODO: fold this into bootstrap
static void setup_private_pts()
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

/*
 * Setup mount profiles as described by snapd.
 *
 * This function reads /var/lib/snapd/mount/$security_tag.fstab as a fstab(5) file
 * and executes the mount requests described there.
 *
 * Currently only bind mounts are allowed. All bind mounts are read only by
 * default though the `rw` flag can be used.
 *
 * This function is called with the rootfs being "consistent" so that it is
 * either the core snap on an all-snap system or the core snap + punched holes
 * on a classic system.
 **/
static void sc_setup_mount_profiles(const char *snap_name)
{
	debug("%s: %s", __FUNCTION__, snap_name);

	FILE *f __attribute__ ((cleanup(sc_cleanup_endmntent))) = NULL;
	const char *mount_profile_dir = "/var/lib/snapd/mount";

	char profile_path[PATH_MAX];
	sc_must_snprintf(profile_path, sizeof(profile_path), "%s/snap.%s.fstab",
			 mount_profile_dir, snap_name);

	debug("opening mount profile %s", profile_path);
	f = setmntent(profile_path, "r");
	// it is ok for the file to not exist
	if (f == NULL && errno == ENOENT) {
		debug("mount profile %s doesn't exist, ignoring", profile_path);
		return;
	}
	// however any other error is a real error
	if (f == NULL) {
		die("cannot open %s", profile_path);
	}

	struct mntent *m = NULL;
	while ((m = getmntent(f)) != NULL) {
		debug("read mount entry\n"
		      "\tmnt_fsname: %s\n"
		      "\tmnt_dir: %s\n"
		      "\tmnt_type: %s\n"
		      "\tmnt_opts: %s\n"
		      "\tmnt_freq: %d\n"
		      "\tmnt_passno: %d",
		      m->mnt_fsname, m->mnt_dir, m->mnt_type,
		      m->mnt_opts, m->mnt_freq, m->mnt_passno);
		int flags = MS_BIND | MS_RDONLY | MS_NODEV | MS_NOSUID;
		debug("initial flags are: bind,ro,nodev,nosuid");
		if (strcmp(m->mnt_type, "none") != 0) {
			die("cannot honor mount profile, only 'none' filesystem type is supported");
		}
		if (hasmntopt(m, "bind") == NULL) {
			die("cannot honor mount profile, the bind mount flag is mandatory");
		}
		if (hasmntopt(m, "rw") != NULL) {
			flags &= ~MS_RDONLY;
		}
		sc_do_mount(m->mnt_fsname, m->mnt_dir, NULL, flags, NULL);
	}
}

struct sc_mount {
	const char *path;
	bool is_bidirectional;
};

struct sc_mount_config {
	const char *rootfs_dir;
	// The struct is terminated with an entry with NULL path.
	const struct sc_mount *mounts;
	bool on_classic;
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
	char src[PATH_MAX];
	char dst[PATH_MAX];
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
	// Make the scratch directory recursively private. Nothing done there will
	// be shared with any peer group, This effectively detaches us from the
	// original namespace and coupled with pivot_root below serves as the
	// foundation of the mount sandbox.
	sc_do_mount("none", scratch_dir, NULL, MS_REC | MS_SLAVE, NULL);
	// Bind mount certain directories from the host filesystem to the scratch
	// directory. By default mount events will propagate in both into and out
	// of the peer group. This way the running application can alter any global
	// state visible on the host and in other snaps. This can be restricted by
	// disabling the "is_bidirectional" flag as can be seen below.
	for (const struct sc_mount * mnt = config->mounts; mnt->path != NULL;
	     mnt++) {
		if (mnt->is_bidirectional && mkdir(mnt->path, 0755) < 0 &&
		    errno != EEXIST) {
			die("cannot create %s", mnt->path);
		}
		sc_must_snprintf(dst, sizeof dst, "%s/%s", scratch_dir,
				 mnt->path);
		sc_do_mount(mnt->path, dst, NULL, MS_REC | MS_BIND, NULL);
		if (!mnt->is_bidirectional) {
			// Mount events will only propagate inwards to the namespace. This
			// way the running application cannot alter any global state apart
			// from that of its own snap.
			sc_do_mount("none", dst, NULL, MS_REC | MS_SLAVE, NULL);
		}
	}
	// Since we mounted /etc from the host filesystem to the scratch directory,
	// we may need to put /etc/alternatives from the desired root filesystem
	// (e.g. the core snap) back. This way the behavior of running snaps is not
	// affected by the alternatives directory from the host, if one exists.
	//
	// https://bugs.launchpad.net/snap-confine/+bug/1580018
	const char *etc_alternatives = "/etc/alternatives";
	if (access(etc_alternatives, F_OK) == 0) {
		sc_must_snprintf(src, sizeof src, "%s%s", config->rootfs_dir,
				 etc_alternatives);
		sc_must_snprintf(dst, sizeof dst, "%s%s", scratch_dir,
				 etc_alternatives);
		sc_do_mount(src, dst, NULL, MS_BIND, NULL);
		sc_do_mount("none", dst, NULL, MS_SLAVE, NULL);
	}
	// Bind mount the directory where all snaps are mounted. The location of
	// the this directory on the host filesystem may not match the location in
	// the desired root filesystem. In the "core" and "ubuntu-core" snaps the
	// directory is always /snap. On the host it is a build-time configuration
	// option stored in SNAP_MOUNT_DIR.
	sc_must_snprintf(dst, sizeof dst, "%s/snap", scratch_dir);
	sc_do_mount(SNAP_MOUNT_DIR, dst, NULL, MS_BIND | MS_REC | MS_SLAVE,
		    NULL);
	sc_do_mount("none", dst, NULL, MS_REC | MS_SLAVE, NULL);
	// Create the hostfs directory if one is missing. This directory is a part
	// of packaging now so perhaps this code can be removed later.
	if (access(SC_HOSTFS_DIR, F_OK) != 0) {
		debug("creating missing hostfs directory");
		if (mkdir(SC_HOSTFS_DIR, 0755) != 0) {
			die("cannot perform operation: mkdir %s",
			    SC_HOSTFS_DIR);
		}
	}
	// Ensure that hostfs isgroup owned by root. We may have (now or earlier)
	// created the directory as the user who first ran a snap on a given
	// system and the group identity of that user is visilbe on disk.
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
	if (config->on_classic) {
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
	sc_do_umount(dst, 0);
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
}

/**
 * @path:    a pathname where / replaced with '\0'.
 * @offsetp: pointer to int showing which path segment was last seen.
 *           Updated on return to reflect the next segment.
 * @fulllen: full original path length.
 * Returns a pointer to the next path segment, or NULL if done.
 */
static char * __attribute__ ((used))
    get_nextpath(char *path, size_t * offsetp, size_t fulllen)
{
	int offset = *offsetp;

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
static bool __attribute__ ((used))
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

void sc_populate_mount_ns(const char *snap_name)
{
	// Get the current working directory before we start fiddling with
	// mounts and possibly pivot_root.  At the end of the whole process, we
	// will try to re-locate to the same directory (if possible).
	char *vanilla_cwd __attribute__ ((cleanup(sc_cleanup_string))) = NULL;
	vanilla_cwd = get_current_dir_name();
	if (vanilla_cwd == NULL) {
		die("cannot get the current working directory");
	}
	// Remember if we are on classic, some things behave differently there.
	bool on_classic = is_running_on_classic_distribution();
	if (on_classic) {
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
			{"/lib/modules"},	// access to the modules of the running kernel
			{"/usr/src"},	// FIXME: move to SecurityMounts in system-trace interface
			{"/var/log"},	// FIXME: move to SecurityMounts in log-observe interface
#ifdef MERGED_USR
			{"/run/media", true},	// access to the users removable devices
#else
			{"/media", true},	// access to the users removable devices
#endif				// MERGED_USR
			{"/run/netns", true},	// access to the 'ip netns' network namespaces
			{},
		};
		struct sc_mount_config classic_config = {
			.rootfs_dir = sc_get_outer_core_mount_point(),
			.mounts = mounts,
			.on_classic = true,
		};
		sc_bootstrap_mount_namespace(&classic_config);
	} else {
		// This is what happens on an all-snap system. The rootfs we start with
		// is the real outer rootfs.  There are no unidirectional bind mounts
		// needed because everything is already OK. We still keep the
		// bidirectional /media mount point so that snaps designed for mounting
		// filesystems can use that space for whatever they need.
		const struct sc_mount mounts[] = {
			{"/media", true},
			{"/run/netns", true},
			{},
		};
		struct sc_mount_config all_snap_config = {
			.rootfs_dir = "/",
			.mounts = mounts,
		};
		sc_bootstrap_mount_namespace(&all_snap_config);
	}

	// set up private mounts
	// TODO: rename this and fold it into bootstrap
	setup_private_mount(snap_name);

	// set up private /dev/pts
	// TODO: fold this into bootstrap
	setup_private_pts();

	// setup quirks for specific snaps
	if (on_classic) {
		sc_setup_quirks();
	}
	// setup the security backend bind mounts
	sc_setup_mount_profiles(snap_name);

	// Try to re-locate back to vanilla working directory. This can fail
	// because that directory is no longer present.
	if (chdir(vanilla_cwd) != 0) {
		debug("cannot remain in %s, moving to the void directory",
		      vanilla_cwd);
		if (chdir(SC_VOID_DIR) != 0) {
			die("cannot change directory to %s", SC_VOID_DIR);
		}
		debug("successfully moved to %s", SC_VOID_DIR);
	}
}

static bool is_mounted_with_shared_option(const char *dir)
    __attribute__ ((nonnull(1)));

static bool is_mounted_with_shared_option(const char *dir)
{
	struct sc_mountinfo *sm
	    __attribute__ ((cleanup(sc_cleanup_mountinfo))) = NULL;
	sm = sc_parse_mountinfo(NULL);
	if (sm == NULL) {
		die("cannot parse /proc/self/mountinfo");
	}
	struct sc_mountinfo_entry *entry = sc_first_mountinfo_entry(sm);
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

void sc_ensure_shared_snap_mount()
{
	if (!is_mounted_with_shared_option("/")
	    && !is_mounted_with_shared_option("/snap")) {
		sc_do_mount("/snap", "/snap", "none", MS_BIND | MS_REC, 0);
		sc_do_mount("none", "/snap", NULL, MS_SHARED | MS_REC, NULL);
	}
}
