/*
 * Copyright (C) 2015-2018 Canonical Ltd
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
#ifdef HAVE_CONFIG_H
#include "config.h"
#endif

#include <errno.h>
#include <fcntl.h>
#include <glob.h>
#include <sched.h>
#include <signal.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/capability.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/time.h>
#include <unistd.h>

#include "../libsnap-confine-private/apparmor-support.h"
#include "../libsnap-confine-private/cgroup-freezer-support.h"
#include "../libsnap-confine-private/cgroup-support.h"
#include "../libsnap-confine-private/classic.h"
#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/feature.h"
#include "../libsnap-confine-private/infofile.h"
#include "../libsnap-confine-private/locking.h"
#include "../libsnap-confine-private/secure-getenv.h"
#include "../libsnap-confine-private/snap.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/tool.h"
#include "../libsnap-confine-private/utils.h"
#include "cookie-support.h"
#include "mount-support.h"
#include "ns-support.h"
#include "seccomp-support.h"
#include "snap-confine-args.h"
#include "snap-confine-invocation.h"
#include "udev-support.h"
#include "user-support.h"
#ifdef HAVE_SELINUX
#include "selinux-support.h"
#endif

// sc_maybe_fixup_permissions fixes incorrect permissions
// inside the mount namespace for /var/lib. Before 1ccce4
// this directory was created with permissions 1777.
static void sc_maybe_fixup_permissions(void)
{
	int fd SC_CLEANUP(sc_cleanup_close) = -1;
	struct stat buf;
	fd = open("/var/lib", O_PATH | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
	if (fd < 0) {
		die("cannot open /var/lib");
	}
	if (fstat(fd, &buf) < 0) {
		die("cannot stat /var/lib");
	}
	if ((buf.st_mode & 0777) == 0777) {
		if (fchmod(fd, 0755) != 0) {
			die("cannot chmod /var/lib");
		}
		if (fchown(fd, 0, 0) != 0) {
			die("cannot chown /var/lib");
		}
	}
}

// sc_maybe_fixup_udev will remove incorrectly created udev tags
// that cause libudev on 16.04 to fail with "udev_enumerate_scan failed".
// See also:
// https://forum.snapcraft.io/t/weird-udev-enumerate-error/2360/17
static void sc_maybe_fixup_udev(void)
{
	glob_t glob_res SC_CLEANUP(globfree) = {
		.gl_pathv = NULL,.gl_pathc = 0,.gl_offs = 0,
	};
	const char *glob_pattern = "/run/udev/tags/snap_*/*nvidia*";
	int err = glob(glob_pattern, 0, NULL, &glob_res);
	if (err == GLOB_NOMATCH) {
		return;
	}
	if (err != 0) {
		die("cannot search using glob pattern %s: %d",
		    glob_pattern, err);
	}
	// kill bogus udev tags for nvidia. They confuse udev, this
	// undoes the damage from github.com/snapcore/snapd/pull/3671.
	//
	// The udev tagging of nvidia got reverted in:
	// https://github.com/snapcore/snapd/pull/4022
	// but leftover files need to get removed or apps won't start
	for (size_t i = 0; i < glob_res.gl_pathc; ++i) {
		unlink(glob_res.gl_pathv[i]);
	}
}

/**
 * sc_preserved_process_state remembers clobbered state to restore.
 *
 * The umask is preserved and restored to ensure consistent permissions for
 * runtime system. The value is preserved and restored perfectly.
**/
typedef struct sc_preserved_process_state {
	mode_t orig_umask;
	int orig_cwd_fd;
	struct stat file_info_orig_cwd;
} sc_preserved_process_state;

/**
 * sc_preserve_and_sanitize_process_state sanitizes process state.
 *
 * The following process state is sanitized:
 *  - the umask is set to 0
 *  - the current working directory is set to /
 *
 * The original values are stored to be restored later. Currently only the
 * umask is altered. It is set to zero to make the ownership of created files
 * and directories more predictable.
**/
static void sc_preserve_and_sanitize_process_state(sc_preserved_process_state
						   *proc_state)
{
	/* Reset umask to zero, storing the old value. */
	proc_state->orig_umask = umask(0);
	debug("umask reset, old umask was %#4o", proc_state->orig_umask);
	/* Remember a file descriptor corresponding to the original working
	 * directory. This is an O_PATH file descriptor. The descriptor is
	 * used as explained below. */
	proc_state->orig_cwd_fd =
	    openat(AT_FDCWD, ".",
		   O_PATH | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
	if (proc_state->orig_cwd_fd < 0) {
		die("cannot open path of the current working directory");
	}
	if (fstat(proc_state->orig_cwd_fd, &proc_state->file_info_orig_cwd) < 0) {
		die("cannot stat path of the current working directory");
	}
	/* Move to the root directory. */
	if (chdir("/") < 0) {
		die("cannot move to /");
	}
}

/**
 *  sc_restore_process_state restores values stored earlier.
**/
static void sc_restore_process_state(const sc_preserved_process_state
				     *proc_state)
{
	/* Restore original umask */
	umask(proc_state->orig_umask);
	debug("umask restored to %#4o", proc_state->orig_umask);

	/* Restore original current working directory.
	 *
	 * This part is more involved for the following reasons. While we hold an
	 * O_PATH file descriptor that still points to the original working
	 * directory, that directory may not be representable in the target mount
	 * namespace. A quick example may be /custom that exists on the host but
	 * not in the base snap of the application.
	 *
	 * Also consider when the path of the original working directory now
	 * maps to a different inode we cannot use fchdir(2). One example of
	 * that is the /tmp directory, which exists in both the host mount
	 * namespace and the per-snap mount namespace but actually represents a
	 * different directory.
	 **/

	/* Read the target of symlink at /proc/self/fd/<fd-of-orig-cwd> */
	char fd_path[PATH_MAX] = { 0 };
	char orig_cwd[PATH_MAX] = { 0 };
	ssize_t nread;
	/* If the original working directory cannot be used for whatever reason then
	 * move the process to a special void directory. */
	const char *sc_void_dir = "/var/lib/snapd/void";
	int void_dir_fd SC_CLEANUP(sc_cleanup_close) = -1;

	sc_must_snprintf(fd_path, sizeof fd_path, "/proc/self/fd/%d",
			 proc_state->orig_cwd_fd);
	nread = readlink(fd_path, orig_cwd, sizeof orig_cwd);
	if (nread < 0) {
		die("cannot read symbolic link target %s", fd_path);
	}
	if (nread == sizeof orig_cwd) {
		die("cannot fit symbolic link target %s", fd_path);
	}
	orig_cwd[nread] = 0;

	/* Open path corresponding to the original working directory in the
	 * execution environment. This may normally fail if the path no longer
	 * exists here, this is not a fatal error. It may also fail if we don't
	 * have permissions to view that path, that is not a fatal error either. */
	int inner_cwd_fd SC_CLEANUP(sc_cleanup_close) = -1;
	inner_cwd_fd =
	    open(orig_cwd, O_PATH | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
	if (inner_cwd_fd < 0) {
		if (errno == EPERM || errno == EACCES || errno == ENOENT) {
			debug
			    ("cannot open path of the original working directory %s",
			     orig_cwd);
			goto the_void;
		}
		/* Any error other than the three above is unexpected. */
		die("cannot open path of the original working directory %s",
		    orig_cwd);
	}

	/* The original working directory exists in the execution environment
	 * which lets us check if it points to the same inode as before. */
	struct stat file_info_inner;
	if (fstat(inner_cwd_fd, &file_info_inner) < 0) {
		die("cannot stat path of working directory in the execution environment");
	}

	/* Note that we cannot use proc_state->orig_cwd_fd as that points to the
	 * directory but in another mount namespace and using that causes
	 * weird and undesired effects.
	 *
	 * By the time this code runs we are already running as the
	 * designated user so UNIX permissions are in effect. */
	if (fchdir(inner_cwd_fd) < 0) {
		if (errno == EPERM || errno == EACCES) {
			debug("cannot access original working directory %s",
			      orig_cwd);
			goto the_void;
		}
		die("cannot restore original working directory via path");
	}
	/* The distinction below is only logged and not acted upon. Perhaps someday
	 * this will be somehow communicated to cooperating applications that can
	 * instruct the user and avoid potential confusion. This mostly applies to
	 * tools that are invoked from /tmp. */
	if (proc_state->file_info_orig_cwd.st_dev ==
	    file_info_inner.st_dev
	    && proc_state->file_info_orig_cwd.st_ino ==
	    file_info_inner.st_ino) {
		/* The path of the original working directory points to the same
		 * inode as before. */
		debug("working directory restored to %s", orig_cwd);
	} else {
		/* The path of the original working directory points to a different
		 * inode inside inside the execution environment than the host
		 * environment. */
		debug("working directory re-interpreted to %s", orig_cwd);
	}
	return;
 the_void:
	/* The void directory may be absent. On core18 system, and other
	 * systems using bootable base snap coupled with snapd snap, the
	 * /var/lib/snapd directory structure is not provided with packages but
	 * created on demand. */
	void_dir_fd = open(sc_void_dir,
			   O_DIRECTORY | O_PATH | O_NOFOLLOW | O_CLOEXEC);
	if (void_dir_fd < 0 && errno == ENOENT) {
		if (mkdir(sc_void_dir, 0111) < 0) {
			die("cannot create void directory: %s", sc_void_dir);
		}
		if (lchown(sc_void_dir, 0, 0) < 0) {
			die("cannot change ownership of void directory %s",
			    sc_void_dir);
		}
		void_dir_fd = open(sc_void_dir,
				   O_DIRECTORY | O_PATH | O_NOFOLLOW |
				   O_CLOEXEC);
	}
	if (void_dir_fd < 0) {
		die("cannot open the void directory %s", sc_void_dir);
	}
	if (fchdir(void_dir_fd) < 0) {
		die("cannot move to void directory %s", sc_void_dir);
	}
	debug("the process has been placed in the special void directory");
}

static void log_startup_stage(const char *stage)
{
	if (!sc_is_debug_enabled()) {
		return;
	}
	struct timeval tv;
	gettimeofday(&tv, NULL);
	debug("-- snap startup {\"stage\":\"%s\", \"time\":\"%lu.%06lu\"}",
	      stage, tv.tv_sec, tv.tv_usec);
}

/**
 *  sc_cleanup_preserved_process_state releases system resources.
**/
static void sc_cleanup_preserved_process_state(sc_preserved_process_state
					       *proc_state)
{
	sc_cleanup_close(&proc_state->orig_cwd_fd);
}

static void enter_classic_execution_environment(const sc_invocation * inv,
						gid_t real_gid,
						gid_t saved_gid);
static void enter_non_classic_execution_environment(sc_invocation * inv,
						    struct sc_apparmor *aa,
						    uid_t real_uid,
						    gid_t real_gid,
						    gid_t saved_gid);

int main(int argc, char **argv)
{
	log_startup_stage("snap-confine enter");
	// Use our super-defensive parser to figure out what we've been asked to do.
	sc_error *err = NULL;
	struct sc_args *args SC_CLEANUP(sc_cleanup_args) = NULL;
	sc_preserved_process_state proc_state
	    SC_CLEANUP(sc_cleanup_preserved_process_state) = {
		.orig_umask = 0,.orig_cwd_fd = -1
	};
	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	sc_die_on_error(err);

	// Remember certain properties of the process that are clobbered by
	// snap-confine during execution. Those are restored just before calling
	// execv.
	sc_preserve_and_sanitize_process_state(&proc_state);

	// We've been asked to print the version string so let's just do that.
	if (sc_args_is_version_query(args)) {
		printf("%s %s\n", PACKAGE, PACKAGE_VERSION);
		return 0;
	}

	/* Collect all invocation parameters. This gives us authoritative
	 * information about what needs to be invoked and how. The data comes
	 * from either the environment or from command line arguments */
	sc_invocation SC_CLEANUP(sc_cleanup_invocation) invocation;
	const char *snap_instance_name_env = getenv("SNAP_INSTANCE_NAME");
	if (snap_instance_name_env == NULL) {
		die("SNAP_INSTANCE_NAME is not set");
	}
	sc_init_invocation(&invocation, args, snap_instance_name_env);

	// Who are we?
	uid_t real_uid, effective_uid, saved_uid;
	gid_t real_gid, effective_gid, saved_gid;
	if (getresuid(&real_uid, &effective_uid, &saved_uid) != 0) {
		die("getresuid failed");
	}
	if (getresgid(&real_gid, &effective_gid, &saved_gid) != 0) {
		die("getresgid failed");
	}
	debug("ruid: %d, euid: %d, suid: %d",
	      real_uid, effective_uid, saved_uid);
	debug("rgid: %d, egid: %d, sgid: %d",
	      real_gid, effective_gid, saved_gid);

	// snap-confine needs to run as root for cgroup/udev/mount/apparmor/etc setup.
	if (effective_uid != 0) {
		die("need to run as root or suid");
	}

	char *snap_context SC_CLEANUP(sc_cleanup_string) = NULL;
	// Do no get snap context value if running a hook (we don't want to overwrite hook's SNAP_COOKIE)
	if (!sc_is_hook_security_tag(invocation.security_tag)) {
		sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
		snap_context =
		    sc_cookie_get_from_snapd(invocation.snap_instance, &err);
		/* While the cookie is normally present due to various protection
		 * mechanisms ensuring its creation from snapd, we are not considering
		 * it a critical error for snap-confine in the case it is absent. When
		 * absent snaps attempting to utilize snapctl to interact with snapd
		 * will fail but it is more important to run a little than break
		 * entirely in case snapd-side code is incorrect. Therefore error
		 * information is collected but discarded. */
	}

	struct sc_apparmor apparmor;
	sc_init_apparmor_support(&apparmor);
	if (!apparmor.is_confined && apparmor.mode != SC_AA_NOT_APPLICABLE
	    && getuid() != 0 && geteuid() == 0) {
		// Refuse to run when this process is running unconfined on a system
		// that supports AppArmor when the effective uid is root and the real
		// id is non-root.  This protects against, for example, unprivileged
		// users trying to leverage the snap-confine in the core snap to
		// escalate privileges.
		errno = 0;	// errno is insignificant here
		die("snap-confine has elevated permissions and is not confined"
		    " but should be. Refusing to continue to avoid"
		    " permission escalation attacks\n"
		    "Please make sure that the snapd.apparmor service is enabled and started.");
	}

	log_startup_stage("snap-confine mount namespace start");

	/* perform global initialization of mount namespace support for non-classic
	 * snaps or both classic and non-classic when parallel-instances feature is
	 * enabled */
	if (!invocation.classic_confinement ||
	    sc_feature_enabled(SC_FEATURE_PARALLEL_INSTANCES)) {

		/* snap-confine uses privately-shared /run/snapd/ns to store bind-mounted
		 * mount namespaces of each snap. In the case that snap-confine is invoked
		 * from the mount namespace it typically constructs, the said directory
		 * does not contain mount entries for preserved namespaces as those are
		 * only visible in the main, outer namespace.
		 *
		 * In order to operate in such an environment snap-confine must first
		 * re-associate its own process with another namespace in which the
		 * /run/snapd/ns directory is visible. The most obvious candidate is pid
		 * one, which definitely doesn't run in a snap-specific namespace, has a
		 * predictable PID and is long lived.
		 */
		sc_reassociate_with_pid1_mount_ns();
		// Do global initialization:
		int global_lock_fd = sc_lock_global();
		// Ensure that "/" or "/snap" is mounted with the
		// "shared" option on legacy systems, see LP:#1668659
		debug("ensuring that snap mount directory is shared");
		sc_ensure_shared_snap_mount();
		unsigned int experimental_features = 0;
		if (sc_feature_enabled(SC_FEATURE_PARALLEL_INSTANCES)) {
			experimental_features |= SC_FEATURE_PARALLEL_INSTANCES;
		}
		sc_initialize_mount_ns(experimental_features);
		sc_unlock(global_lock_fd);
	}

	if (invocation.classic_confinement) {
		enter_classic_execution_environment(&invocation, real_gid,
						    saved_gid);
	} else {
		enter_non_classic_execution_environment(&invocation,
							&apparmor,
							real_uid,
							real_gid, saved_gid);
	}

	log_startup_stage("snap-confine mount namespace finish");

	// Temporarily drop privileges back to the calling user until we can
	// permanently drop (which we can't do just yet due to seccomp, see
	// below).
	sc_identity real_user_identity = {
		.uid = real_uid,
		.gid = real_gid,
		.change_uid = 1,
		.change_gid = 1,
	};
	sc_set_effective_identity(real_user_identity);
	// Ensure that the user data path exists. When creating it use the identity
	// of the calling user (by using real user and group identifiers). This
	// allows the creation of directories inside ~/ on NFS with root_squash
	// attribute.
	setup_user_data();
#if 0
	setup_user_xdg_runtime_dir();
#endif
	// https://wiki.ubuntu.com/SecurityTeam/Specifications/SnappyConfinement
	sc_maybe_aa_change_onexec(&apparmor, invocation.security_tag);
#ifdef HAVE_SELINUX
	// For classic and confined snaps
	sc_selinux_set_snap_execcon();
#endif
	if (snap_context != NULL) {
		setenv("SNAP_COOKIE", snap_context, 1);
		// for compatibility, if facing older snapd.
		setenv("SNAP_CONTEXT", snap_context, 1);
	}
	// Normally setuid/setgid not only permanently drops the UID/GID, but
	// also clears the capabilities bounding sets (see "Effect of user ID
	// changes on capabilities" in 'man capabilities'). To load a seccomp
	// profile, we need either CAP_SYS_ADMIN or PR_SET_NO_NEW_PRIVS. Since
	// NNP causes issues with AppArmor and exec transitions in certain
	// snapd interfaces, keep CAP_SYS_ADMIN temporarily when we are
	// permanently dropping privileges.
	if (getresuid(&real_uid, &effective_uid, &saved_uid) != 0) {
		die("getresuid failed");
	}
	debug("ruid: %d, euid: %d, suid: %d",
	      real_uid, effective_uid, saved_uid);
	struct __user_cap_header_struct hdr =
	    { _LINUX_CAPABILITY_VERSION_3, 0 };
	struct __user_cap_data_struct cap_data[2] = { {0} };

	// At this point in time, if we are going to permanently drop our
	// effective_uid will not be '0' but our saved_uid will be '0'. Detect
	// and save when we are in the this state so know when to setup the
	// capabilities bounding set, regain CAP_SYS_ADMIN and later drop it.
	bool keep_sys_admin = effective_uid != 0 && saved_uid == 0;
	if (keep_sys_admin) {
		debug("setting capabilities bounding set");
		// clear all 32 bit caps but SYS_ADMIN, with none inheritable
		cap_data[0].effective = CAP_TO_MASK(CAP_SYS_ADMIN);
		cap_data[0].permitted = cap_data[0].effective;
		cap_data[0].inheritable = 0;
		// clear all 64 bit caps
		cap_data[1].effective = 0;
		cap_data[1].permitted = 0;
		cap_data[1].inheritable = 0;
		if (capset(&hdr, cap_data) != 0) {
			die("capset failed");
		}
	}
	// Permanently drop if not root
	if (effective_uid == 0) {
		// Note that we do not call setgroups() here because its ok
		// that the user keeps the groups he already belongs to
		if (setgid(real_gid) != 0)
			die("setgid failed");
		if (setuid(real_uid) != 0)
			die("setuid failed");

		if (real_gid != 0 && (getuid() == 0 || geteuid() == 0))
			die("permanently dropping privs did not work");
		if (real_uid != 0 && (getgid() == 0 || getegid() == 0))
			die("permanently dropping privs did not work");
	}
	// Now that we've permanently dropped, regain SYS_ADMIN
	if (keep_sys_admin) {
		debug("regaining SYS_ADMIN");
		cap_data[0].effective = CAP_TO_MASK(CAP_SYS_ADMIN);
		cap_data[0].permitted = cap_data[0].effective;
		if (capset(&hdr, cap_data) != 0) {
			die("capset regain failed");
		}
	}
	// Now that we've dropped and regained SYS_ADMIN, we can load the
	// seccomp profiles.
	if (sc_apply_seccomp_profile_for_security_tag(invocation.security_tag)) {
		// If the process is not explicitly unconfined then load the
		// global profile as well.
		sc_apply_global_seccomp_profile();
	}
	// Even though we set inheritable to 0, let's clear SYS_ADMIN
	// explicitly
	if (keep_sys_admin) {
		debug("clearing SYS_ADMIN");
		cap_data[0].effective = 0;
		cap_data[0].permitted = cap_data[0].effective;
		if (capset(&hdr, cap_data) != 0) {
			die("capset clear failed");
		}
	}
	// and exec the new executable
	argv[0] = (char *)invocation.executable;
	debug("execv(%s, %s...)", invocation.executable, argv[0]);
	for (int i = 1; i < argc; ++i) {
		debug(" argv[%i] = %s", i, argv[i]);
	}
	// Restore process state that was recorded earlier.
	sc_restore_process_state(&proc_state);
	log_startup_stage("snap-confine to snap-exec");
	execv(invocation.executable, (char *const *)&argv[0]);
	perror("execv failed");
	return 1;
}

static void enter_classic_execution_environment(const sc_invocation *inv,
						gid_t real_gid, gid_t saved_gid)
{
	/* with parallel-instances enabled, main() reassociated with the mount ns of
	 * PID 1 to make /run/snapd/ns visible */

	/* 'classic confinement' is designed to run without the sandbox inside the
	 * shared namespace. Specifically:
	 * - snap-confine skips using the snap-specific, private, mount namespace
	 * - snap-confine skips using device cgroups
	 * - snapd sets up a lenient AppArmor profile for snap-confine to use
	 * - snapd sets up a lenient seccomp profile for snap-confine to use
	 */
	debug("preparing classic execution environment");

	if (!sc_feature_enabled(SC_FEATURE_PARALLEL_INSTANCES)) {
		return;
	}

	/* all of the following code is experimental and part of parallel instances
	 * of classic snaps support */

	debug
	    ("(experimental) unsharing the mount namespace (per-classic-snap)");

	/* Construct a mount namespace where the snap instance directories are
	 * visible under the regular snap name. In order to do that we will:
	 *
	 * - convert SNAP_MOUNT_DIR into a mount point (global init)
	 * - convert /var/snap into a mount point (global init)
	 * - always create a new mount namespace
	 * - for snaps with non empty instance key:
	 *   - set slave propagation recursively on SNAP_MOUNT_DIR and /var/snap
	 *   - recursively bind mount SNAP_MOUNT_DIR/<snap>_<key> on top of SNAP_MOUNT_DIR/<snap>
	 *   - recursively bind mount /var/snap/<snap>_<key> on top of /var/snap/<snap>
	 *
	 * The destination directories /var/snap/<snap> and SNAP_MOUNT_DIR/<snap>
	 * are guaranteed to exist and were created during installation of a given
	 * instance.
	 */

	if (unshare(CLONE_NEWNS) < 0) {
		die("cannot unshare the mount namespace for parallel installed classic snap");
	}

	/* Parallel installed classic snap get special handling */
	if (!sc_streq(inv->snap_instance, inv->snap_name)) {
		debug
		    ("(experimental) setting up environment for classic snap instance %s",
		     inv->snap_instance);

		/* set up mappings for snap and data directories */
		sc_setup_parallel_instance_classic_mounts(inv->snap_name,
							  inv->snap_instance);
	}
}

static bool is_device_cgroup_self_managed(const sc_invocation *inv)
{
	char info_path[PATH_MAX] = { 0 };
	sc_must_snprintf(info_path,
			 sizeof info_path,
			 "/var/lib/snapd/cgroup/snap.%s.device",
			 inv->snap_instance);

	FILE *stream SC_CLEANUP(sc_cleanup_file) = NULL;
	stream = fopen(info_path, "r");
	if (stream == NULL && errno == ENOENT) {
		/* File not there, so definitely not self-managed */
		return false;
	}
	if (stream == NULL) {
		die("cannot open %s", info_path);
	}

	char *self_managed_value SC_CLEANUP(sc_cleanup_string) = NULL;
	sc_error *err = NULL;
	if (sc_infofile_get_key
	    (stream, "self-managed", &self_managed_value, &err) < 0) {
		sc_die_on_error(err);
	}

	return sc_streq(self_managed_value, "true");
}

static void enter_non_classic_execution_environment(sc_invocation *inv,
						    struct sc_apparmor *aa,
						    uid_t real_uid,
						    gid_t real_gid,
						    gid_t saved_gid)
{
	// main() reassociated with the mount ns of PID 1 to make /run/snapd/ns
	// visible

	// Find and open snap-update-ns and snap-discard-ns from the same
	// path as where we (snap-confine) were called.
	int snap_update_ns_fd SC_CLEANUP(sc_cleanup_close) = -1;
	snap_update_ns_fd = sc_open_snap_update_ns();
	int snap_discard_ns_fd SC_CLEANUP(sc_cleanup_close) = -1;
	snap_discard_ns_fd = sc_open_snap_discard_ns();

	// Do per-snap initialization.
	int snap_lock_fd = sc_lock_snap(inv->snap_instance);
	debug("initializing mount namespace: %s", inv->snap_instance);
	struct sc_mount_ns *group = NULL;
	group = sc_open_mount_ns(inv->snap_instance);

	// Init and check rootfs_dir, apply any fallback behaviors.
	sc_check_rootfs_dir(inv);

	// Set up a device cgroup, unless the snap has been allowed to manage the
	// device cgroup by itself.
	if (!is_device_cgroup_self_managed(inv)) {
		/** Conditionally create, populate and join the device cgroup. */
		sc_device_cgroup_mode mode = SC_DEVICE_CGROUP_MODE_REQUIRED;

		/* Preserve the legacy behavior of no default device cgroup for snaps
		 * using one of the following bases. Snaps using core24 and later bases
		 * will be placed within a device cgroup. Note that 'bare' base is also
		 * subject to the new behavior. */
		const char *non_required_cgroup_bases[] = {
			"core", "core16", "core18", "core20", "core22",
			NULL,
		};
		for (const char **non_required_on_base =
		     non_required_cgroup_bases; *non_required_on_base != NULL;
		     non_required_on_base++) {
			if (sc_streq
			    (inv->base_snap_name, *non_required_on_base)) {
				debug
				    ("device cgroup not required due to base %s",
				     *non_required_on_base);
				mode = SC_DEVICE_CGROUP_MODE_OPTIONAL;
				break;
			}
		}
		sc_setup_device_cgroup(inv->security_tag, mode);
	} else {
		debug("device cgroup is self-managed by the snap");
	}

	/**
	 * is_normal_mode controls if we should pivot into the base snap.
	 *
	 * There are two modes of execution for snaps that are not using classic
	 * confinement: normal and legacy. The normal mode is where snap-confine
	 * sets up a rootfs and then pivots into it using pivot_root(2). The legacy
	 * mode is when snap-confine just unshares the initial mount namespace,
	 * makes some extra changes but largely runs with what was presented to it
	 * initially.
	 *
	 * Historically the ubuntu-core distribution used the now-legacy mode. This
	 * was sensible then since snaps already (kind of) have the right root
	 * file-system and just need some privacy and isolation features applied.
	 * With the introduction of snaps to classic distributions as well as the
	 * introduction of bases, where each snap can use a different root
	 * filesystem, this lost sensibility and thus became legacy.
	 *
	 * For compatibility with current installations of ubuntu-core
	 * distributions the legacy mode is used when: the distribution is
	 * SC_DISTRO_CORE16 or when the base snap name is not "core" or
	 * "ubuntu-core".
	 *
	 * The SC_DISTRO_CORE16 is applied to systems that boot with the "core",
	 * "ubuntu-core" or "core16" snap. Systems using the "core18" base snap do
	 * not qualify for that classification.
	 **/
	sc_distro distro = sc_classify_distro();
	inv->is_normal_mode = distro != SC_DISTRO_CORE16 ||
	    !sc_streq(inv->orig_base_snap_name, "core");

	/* Read the homedirs configuration: this information is needed both by our
	 * namespace helper (in order to detect if the homedirs are mounted) and by
	 * snap-confine itself to mount the homedirs.
	 */
	sc_invocation_init_homedirs(inv);

	/* Stale mount namespace discarded or no mount namespace to
	   join. We need to construct a new mount namespace ourselves.
	   To capture it we will need a helper process so make one. */
	sc_fork_helper(group, aa);
	int retval = sc_join_preserved_ns(group, aa, inv, snap_discard_ns_fd);
	if (retval == ESRCH) {
		/* Create and populate the mount namespace. This performs all
		   of the bootstrapping mounts, pivots into the new root filesystem and
		   applies the per-snap mount profile using snap-update-ns. */
		debug("unsharing the mount namespace (per-snap)");
		if (unshare(CLONE_NEWNS) < 0) {
			die("cannot unshare the mount namespace");
		}
		sc_populate_mount_ns(aa, snap_update_ns_fd, inv, real_gid,
				     saved_gid);
		sc_store_ns_info(inv);

		/* Preserve the mount namespace. */
		sc_preserve_populated_mount_ns(group);
	}

	/* Older versions of snap-confine created incorrect 777 permissions
	   for /var/lib and we need to fixup for systems that had their NS created
	   with an old version. */
	sc_maybe_fixup_permissions();
	sc_maybe_fixup_udev();

	/* User mount profiles only apply to non-root users. */
	if (real_uid != 0) {
		debug("joining preserved per-user mount namespace");
		retval =
		    sc_join_preserved_per_user_ns(group, inv->snap_instance);
		if (retval == ESRCH) {
			debug("unsharing the mount namespace (per-user)");
			if (unshare(CLONE_NEWNS) < 0) {
				die("cannot unshare the mount namespace");
			}
			sc_setup_user_mounts(aa, snap_update_ns_fd,
					     inv->snap_instance);
			/* Preserve the mount per-user namespace. But only if the
			 * experimental feature is enabled. This way if the feature is
			 * disabled user mount namespaces will still exist but will be
			 * entirely ephemeral. In addition the call
			 * sc_join_preserved_user_ns() will never find a preserved mount
			 * namespace and will always enter this code branch. */
			if (sc_feature_enabled
			    (SC_FEATURE_PER_USER_MOUNT_NAMESPACE)) {
				sc_preserve_populated_per_user_mount_ns(group);
			} else {
				debug
				    ("NOT preserving per-user mount namespace");
			}
		}
	}
	// With cgroups v1, associate each snap process with a dedicated
	// snap freezer cgroup and snap pids cgroup. All snap processes
	// belonging to one snap share the freezer cgroup. All snap
	// processes belonging to one app or one hook share the pids cgroup.
	//
	// This simplifies testing if any processes belonging to a given snap are
	// still alive as well as to properly account for each application and
	// service.
	//
	// Note that with cgroups v2 there is no separate freeezer controller,
	// but the freezer is associated with each group. The call chain when
	// starting the snap application has already ensure that the process has
	// been put in a dedicated group.
	if (!sc_cgroup_is_v2()) {
		sc_cgroup_freezer_join(inv->snap_instance, getpid());
	}

	sc_unlock(snap_lock_fd);

	sc_close_mount_ns(group);

	// Reset path as we cannot rely on the path from the host OS to make sense.
	// The classic distribution may use any PATH that makes sense but we cannot
	// assume it makes sense for the core snap layout. Note that the /usr/local
	// directories are explicitly left out as they are not part of the core
	// snap.
	debug("resetting PATH to values in sync with core snap");
	setenv("PATH",
	       "/usr/local/sbin:"
	       "/usr/local/bin:"
	       "/usr/sbin:"
	       "/usr/bin:"
	       "/sbin:" "/bin:" "/usr/games:" "/usr/local/games", 1);
	// Ensure we set the various TMPDIRs to /tmp. One of the parts of setting
	// up the mount namespace is to create a private /tmp directory (this is
	// done in sc_populate_mount_ns() above). The host environment may point to
	// a directory not accessible by snaps so we need to reset it here.
	const char *tmpd[] = { "TMPDIR", "TEMPDIR", NULL };
	int i;
	for (i = 0; tmpd[i] != NULL; i++) {
		if (setenv(tmpd[i], "/tmp", 1) != 0) {
			die("cannot set environment variable '%s'", tmpd[i]);
		}
	}
}
