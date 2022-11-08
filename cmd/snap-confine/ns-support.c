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

#include "ns-support.h"

#ifdef HAVE_CONFIG_H
#include "config.h"
#endif

#include <errno.h>
#include <fcntl.h>
#include <linux/magic.h>
#include <sched.h>
#include <signal.h>
#include <string.h>
#include <sys/eventfd.h>
#include <sys/file.h>
#include <sys/mount.h>
#include <sys/prctl.h>
#include <sys/stat.h>
#include <sys/sysmacros.h>
#include <sys/types.h>
#include <sys/vfs.h>
#include <sys/wait.h>
#include <unistd.h>

#include "../libsnap-confine-private/cgroup-freezer-support.h"
#include "../libsnap-confine-private/cgroup-support.h"
#include "../libsnap-confine-private/classic.h"
#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/feature.h"
#include "../libsnap-confine-private/infofile.h"
#include "../libsnap-confine-private/locking.h"
#include "../libsnap-confine-private/mountinfo.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/tool.h"
#include "../libsnap-confine-private/utils.h"
#include "user-support.h"
#include "mount-support.h"

/**
 * Directory where snap-confine keeps namespace files.
 **/
#define SC_NS_DIR "/run/snapd/ns"

/**
 * Effective value of SC_NS_DIR.
 *
 * We use 'const char *' so we can update sc_ns_dir in the testsuite
 **/
static const char *sc_ns_dir = SC_NS_DIR;

enum {
	HELPER_CMD_EXIT,
	HELPER_CMD_CAPTURE_MOUNT_NS,
	HELPER_CMD_CAPTURE_PER_USER_MOUNT_NS,
};

void sc_reassociate_with_pid1_mount_ns(void)
{
	int init_mnt_fd SC_CLEANUP(sc_cleanup_close) = -1;
	int self_mnt_fd SC_CLEANUP(sc_cleanup_close) = -1;
	const char *path_pid_1 = "/proc/1/ns/mnt";
	const char *path_pid_self = "/proc/self/ns/mnt";

	init_mnt_fd = open(path_pid_1,
			   O_RDONLY | O_CLOEXEC | O_NOFOLLOW | O_PATH);
	if (init_mnt_fd < 0) {
		die("cannot open path %s", path_pid_1);
	}
	self_mnt_fd = open(path_pid_self,
			   O_RDONLY | O_CLOEXEC | O_NOFOLLOW | O_PATH);
	if (self_mnt_fd < 0) {
		die("cannot open path %s", path_pid_1);
	}
	char init_buf[128] = { 0 };
	char self_buf[128] = { 0 };
	memset(init_buf, 0, sizeof init_buf);
	if (readlinkat(init_mnt_fd, "", init_buf, sizeof init_buf) < 0) {
		if (errno == ENOENT) {
			// According to namespaces(7) on a pre 3.8 kernel the namespace
			// files are hardlinks, not symlinks. If that happens readlinkat
			// fails with ENOENT. As a quick workaround for this special-case
			// functionality, just bail out and do nothing without raising an
			// error.
			return;
		}
		die("cannot read mount namespace identifier of pid 1");
	}
	memset(self_buf, 0, sizeof self_buf);
	if (readlinkat(self_mnt_fd, "", self_buf, sizeof self_buf) < 0) {
		die("cannot read mount namespace identifier of the current process");
	}
	if (memcmp(init_buf, self_buf, sizeof init_buf) != 0) {
		debug("moving to mount namespace of pid 1");
		// We cannot use O_NOFOLLOW here because that file will always be a
		// symbolic link. We actually want to open it this way.
		int init_mnt_fd_real SC_CLEANUP(sc_cleanup_close) = -1;
		init_mnt_fd_real = open(path_pid_1, O_RDONLY | O_CLOEXEC);
		if (init_mnt_fd_real < 0) {
			die("cannot open %s", path_pid_1);
		}
		if (setns(init_mnt_fd_real, CLONE_NEWNS) < 0) {
			die("cannot join mount namespace of pid 1");
		}
	}
}

void sc_initialize_mount_ns(unsigned int experimental_features)
{
	debug("unsharing snap namespace directory");

	/* Ensure that /run/snapd/ns is a directory. */
	if (sc_nonfatal_mkpath(sc_ns_dir, 0755, 0, 0) < 0) {
		die("cannot create directory %s", sc_ns_dir);
	}

	/* Read and analyze the mount table. We need to see whether /run/snapd/ns
	 * is a mount point with private event propagation. */
	sc_mountinfo *info SC_CLEANUP(sc_cleanup_mountinfo) = NULL;
	info = sc_parse_mountinfo(NULL);
	if (info == NULL) {
		die("cannot parse /proc/self/mountinfo");
	}

	bool is_mnt = false;
	bool is_private = false;
	for (sc_mountinfo_entry * entry = sc_first_mountinfo_entry(info);
	     entry != NULL; entry = sc_next_mountinfo_entry(entry)) {
		/* Find /run/snapd/ns */
		if (!sc_streq(entry->mount_dir, sc_ns_dir)) {
			continue;
		}
		is_mnt = true;
		if (strstr(entry->optional_fields, "shared:") == NULL) {
			/* Mount event propagation is not set to shared, good. */
			is_private = true;
		}
		break;
	}

	if (!is_mnt) {
		if (mount(sc_ns_dir, sc_ns_dir, NULL, MS_BIND | MS_REC, NULL) <
		    0) {
			die("cannot self-bind mount %s", sc_ns_dir);
		}
	}

	if (!is_private) {
		if (mount(NULL, sc_ns_dir, NULL, MS_PRIVATE, NULL) < 0) {
			die("cannot change propagation type to MS_PRIVATE in %s", sc_ns_dir);
		}
	}

	/* code that follows is experimental */
	if (experimental_features & SC_FEATURE_PARALLEL_INSTANCES) {
		// Ensure that SNAP_MOUNT_DIR and /var/snap are shared mount points
		debug
		    ("(experimental) ensuring snap mount and data directories are mount points");
		sc_ensure_snap_dir_shared_mounts();
	}
}

struct sc_mount_ns {
	// Name of the namespace group ($SNAP_NAME).
	char *name;
	// Descriptor to the namespace group control directory.  This descriptor is
	// opened with O_PATH|O_DIRECTORY so it's only used for openat() calls.
	int dir_fd;
	// Pair of descriptors for a pair for a pipe file descriptors (read end,
	// write end) that snap-confine uses to send messages to the helper
	// process and back.
	int pipe_helper[2];
	int pipe_master[2];
	// Identifier of the child process that is used during the one-time (per
	// group) initialization and capture process.
	pid_t child;
};

static struct sc_mount_ns *sc_alloc_mount_ns(void)
{
	struct sc_mount_ns *group = calloc(1, sizeof *group);
	if (group == NULL) {
		die("cannot allocate memory for sc_mount_ns");
	}
	group->dir_fd = -1;
	group->pipe_helper[0] = -1;
	group->pipe_helper[1] = -1;
	group->pipe_master[0] = -1;
	group->pipe_master[1] = -1;
	// Redundant with calloc but some functions check for the non-zero value so
	// I'd like to keep this explicit in the code.
	group->child = 0;
	return group;
}

struct sc_mount_ns *sc_open_mount_ns(const char *group_name)
{
	struct sc_mount_ns *group = sc_alloc_mount_ns();
	group->dir_fd = open(sc_ns_dir,
			     O_DIRECTORY | O_PATH | O_CLOEXEC | O_NOFOLLOW);
	if (group->dir_fd < 0) {
		die("cannot open directory %s", sc_ns_dir);
	}
	group->name = sc_strdup(group_name);
	return group;
}

void sc_close_mount_ns(struct sc_mount_ns *group)
{
	if (group->child != 0) {
		sc_wait_for_helper(group);
	}
	sc_cleanup_close(&group->dir_fd);
	sc_cleanup_close(&group->pipe_master[0]);
	sc_cleanup_close(&group->pipe_master[1]);
	sc_cleanup_close(&group->pipe_helper[0]);
	sc_cleanup_close(&group->pipe_helper[1]);
	free(group->name);
	free(group);
}

static dev_t find_base_snap_device(const char *base_snap_name,
				   const char *base_snap_rev)
{
	// Find the backing device of the base snap.
	// TODO: add support for "try mode" base snaps that also need
	// consideration of the mie->root component.
	dev_t base_snap_dev = 0;
	char base_squashfs_path[PATH_MAX];
	sc_must_snprintf(base_squashfs_path,
			 sizeof base_squashfs_path, "%s/%s/%s",
			 SNAP_MOUNT_DIR, base_snap_name, base_snap_rev);
	sc_mountinfo *mi SC_CLEANUP(sc_cleanup_mountinfo) = NULL;
	mi = sc_parse_mountinfo(NULL);
	if (mi == NULL) {
		die("cannot parse mountinfo of the current process");
	}
	bool found = false;
	for (sc_mountinfo_entry * mie =
	     sc_first_mountinfo_entry(mi); mie != NULL;
	     mie = sc_next_mountinfo_entry(mie)) {
		if (sc_streq(mie->mount_dir, base_squashfs_path)) {
			base_snap_dev = makedev(mie->dev_major, mie->dev_minor);
			debug("block device of snap %s, revision %s is %d:%d",
			      base_snap_name, base_snap_rev, mie->dev_major,
			      mie->dev_minor);
			// Don't break when found, we are interested in the last
			// entry as this is the "effective" one.
			found = true;
		}
	}
	if (!found) {
		die("cannot find mount entry for snap %s revision %s",
		    base_snap_name, base_snap_rev);
	}
	return base_snap_dev;
}

static bool base_snap_device_changed(sc_mountinfo *mi, dev_t base_snap_dev)
{
	sc_mountinfo_entry *mie;

	/* We are looking for a mount entry matching the device ID of the base
	 * snap. We need to take these cases into account:
	 * 1) In the typical case, this will be mounted on the "/" directory.
	 * 2) If the root directory is a tmpfs, the base snap would be mounted
	 *    under /usr.
	 * 3) If the snap has a layout that adds directories or files directly
	 *    under /usr, a writable mimic will be created: /usr will be a tmpfs,
	 *    with all of the original directory entries inside of /usr being
	 *    bind-mounted onto mount-points created into the tmpfs.
	 * In light of the above, we do ignore all tmpfs entries and accept that
	 * our base snap might be mounted under /, /usr, or anywhere under /usr.
	 */
	for (mie = sc_first_mountinfo_entry(mi); mie != NULL;
	     mie = sc_next_mountinfo_entry(mie)) {
		if (sc_streq(mie->fs_type, "tmpfs")) {
			continue;
		}

		if (base_snap_dev == makedev(mie->dev_major, mie->dev_minor) &&
		    (sc_streq(mie->mount_dir, "/") ||
		     sc_streq(mie->mount_dir, "/usr") ||
		     sc_startswith(mie->mount_dir, "/usr/"))) {
			debug("found base snap device %d:%d on %s",
			      mie->dev_major, mie->dev_minor, mie->mount_dir);
			return false;
		}
	}
	debug("base snap device %d:%d not found in existing mount ns",
	      major(base_snap_dev), minor(base_snap_dev));
	return true;
}

static bool homedirs_are_mounted(sc_mountinfo *mi, char **homedirs,
				 int num_homedirs)
{
	if (num_homedirs == 0) {
		return true;
	}

	/* We know that the number of homedirs is not going to be huge, so let's
	 * just allocare this vector on the stack */
	bool homedir_seen[num_homedirs];
	for (int i = 0; i < num_homedirs; i++) {
		homedir_seen[i] = false;
	}

	sc_mountinfo_entry *mie;
	for (mie = sc_first_mountinfo_entry(mi); mie != NULL;
	     mie = sc_next_mountinfo_entry(mie)) {
		for (int i = 0; i < num_homedirs; i++) {
			if (sc_streq(mie->mount_dir, homedirs[i])) {
				homedir_seen[i] = true;
			}
		}
	}

	bool all_seen = true;
	for (int i = 0; i < num_homedirs; i++) {
		if (!homedir_seen[i]) {
			debug("Homedir %s missing from namespace", homedirs[i]);
			all_seen = false;
			break;
		}
	}
	return all_seen;
}

// Inspect the namespace and check if we should discard it.
static bool should_discard_current_ns(const struct sc_invocation *inv,
				      dev_t base_snap_dev)
{
	sc_mountinfo *mi SC_CLEANUP(sc_cleanup_mountinfo) = NULL;

	mi = sc_parse_mountinfo(NULL);
	if (mi == NULL) {
		die("cannot parse mountinfo of the current process");
	}
	// The namespace may become "stale" when the rootfs is not the same
	// device we found above. This will happen whenever the base snap is
	// refreshed since the namespace was first created.
	if (base_snap_device_changed(mi, base_snap_dev)) {
		return true;
	}
	// Another reason for becoming stale is if the homedirs configuration has
	// changed: so this code will check that all homedirs are mounted in the
	// namespace.
	if (!homedirs_are_mounted(mi, inv->homedirs, inv->num_homedirs)) {
		return true;
	}

	return false;
}

enum sc_discard_vote {
	/**
	 * SC_DISCARD_NO denotes that the mount namespace doesn't have to be
	 * discarded. This happens when the base snap has not changed.
	 **/
	SC_DISCARD_NO = 1,
	/**
	 * SC_DISCARD_SHOULD indicates that the mount namespace should be discarded
	 * but may be reused if it is still inhabited by processes. This only
	 * happens when the base snap revision changes but the name of the base
	 * snap is the same as before.
	 **/
	SC_DISCARD_SHOULD = 2,
	/**
	 * SC_DISCARD_MUST indicates that the mount namespace must be discarded
	 * even if it still inhabited by processes. This only happens when the name
	 * of the base snap changes.
	 **/
	SC_DISCARD_MUST = 3,
};

/**
 * is_base_transition returns true if a base transition is occurring.
 *
 * The function inspects /run/snapd/ns/snap.$SNAP_INSTANCE_NAME.info as well
 * as the invocation parameters of snap-confine. If the base snap name, as
 * encoded in the info file and as described by the invocation parameters
 * differ then a base transition is occurring. If the info file is absent or
 * does not record the name of the base snap then transition cannot be
 * detected.
**/
static bool is_base_transition(const sc_invocation *inv)
{
	char info_path[PATH_MAX] = { 0 };
	sc_must_snprintf(info_path,
			 sizeof info_path,
			 "/run/snapd/ns/snap.%s.info", inv->snap_instance);

	FILE *stream SC_CLEANUP(sc_cleanup_file) = NULL;
	stream = fopen(info_path, "r");
	if (stream == NULL && errno == ENOENT) {
		// If the info file is absent then we cannot decide if a transition had
		// occurred. For people upgrading from snap-confine without the info
		// file, that is the best we can do.
		return false;
	}
	if (stream == NULL) {
		die("cannot open %s", info_path);
	}

	char *base_snap_name SC_CLEANUP(sc_cleanup_string) = NULL;
	sc_error *err = NULL;
	if (sc_infofile_get_key
	    (stream, "base-snap-name", &base_snap_name, &err) < 0) {
		sc_die_on_error(err);
	}

	if (base_snap_name == NULL) {
		// If the info file doesn't record the name of the base snap then,
		// again, we cannot decide if a transition had occurred.
		return false;
	}

	return !sc_streq(inv->orig_base_snap_name, base_snap_name);
}

static bool sc_is_mount_ns_in_use(const char *snap_instance);

// The namespace may be stale. To check this we must actually switch into it
// but then we use up our setns call (the kernel misbehaves if we setns twice).
// To work around this we'll fork a child and use it to probe. The child will
// inspect the namespace and send information back via eventfd and then exit
// unconditionally.
static int sc_inspect_and_maybe_discard_stale_ns(int mnt_fd,
						 const sc_invocation *inv,
						 int snap_discard_ns_fd)
{
	char base_snap_rev[PATH_MAX] = { 0 };
	dev_t base_snap_dev;
	int event_fd SC_CLEANUP(sc_cleanup_close) = -1;

	// Read the revision of the base snap by looking at the current symlink.
	if (readlink(inv->rootfs_dir, base_snap_rev, sizeof base_snap_rev) < 0) {
		die("cannot read current revision of snap %s",
		    inv->snap_instance);
	}
	if (base_snap_rev[sizeof base_snap_rev - 1] != '\0') {
		die("cannot read current revision of snap %s: value too long",
		    inv->snap_instance);
	}
	// Find the device that is backing the current revision of the base snap.
	base_snap_dev =
	    find_base_snap_device(inv->base_snap_name, base_snap_rev);

	// Store the PID of this process. This is done instead of calls to
	// getppid() below because then we can reliably track the PID of the
	// parent even if the child process is re-parented.
	pid_t parent = getpid();

	// Create an eventfd for the communication with the child.
	event_fd = eventfd(0, EFD_CLOEXEC);
	if (event_fd < 0) {
		die("cannot create eventfd");
	}
	// Fork a child, it will do the inspection for us.
	pid_t child = fork();
	if (child < 0) {
		die("cannot fork support process");
	}

	if (child == 0) {
		// This is the child process which will inspect the mount namespace.
		//
		// Configure the child to die as soon as the parent dies. In an odd
		// case where the parent is killed then we don't want to complete our
		// task or wait for anything.
		if (prctl(PR_SET_PDEATHSIG, SIGINT, 0, 0, 0) < 0) {
			die("cannot set parent process death notification signal to SIGINT");
		}
		// Check that parent process is still alive. If this is the case then
		// we can *almost* reliably rely on the PR_SET_PDEATHSIG signal to wake
		// us up from eventfd_read() below. In the rare case that the PID
		// numbers overflow and the now-dead parent PID is recycled we will
		// still hang forever on the read from eventfd below.
		if (kill(parent, 0) < 0) {
			switch (errno) {
			case ESRCH:
				debug("parent process has terminated");
				abort();
			default:
				die("cannot confirm that parent process is alive");
				break;
			}
		}

		debug("joining preserved mount namespace for inspection");
		// Move to the mount namespace of the snap we're trying to inspect.
		if (setns(mnt_fd, CLONE_NEWNS) < 0) {
			die("cannot join preserved mount namespace");
		}
		// Check if the namespace needs to be discarded.
		eventfd_t value = SC_DISCARD_NO;
		const char *value_str = "no";

		// TODO: enable this for core distributions. This is complex because on
		// core the rootfs is mounted in initrd and is _not_ changed (no
		// pivot_root) and the base snap is again mounted (2nd time) by
		// systemd. This makes us end up in a situation where the outer base
		// snap will never match the rootfs inside the mount namespace.
		if (inv->is_normal_mode
		    && should_discard_current_ns(inv, base_snap_dev)) {
			value = SC_DISCARD_SHOULD;
			value_str = "should";
		}
		// If the base snap changed, we must discard the mount namespace and
		// start over to allow the newly started process to see the requested
		// base snap. Due to the TODO above always perform explicit transition
		// check to protect against LP:#1819875 and LP:#1861901
		if (is_base_transition(inv)) {
			// The base snap has changed. We must discard ...
			value = SC_DISCARD_MUST;
			value_str = "must";
		}
		// Send this back to the parent: 3 - force discard 2 - prefer discard, 1 - keep.
		// Note that we cannot just use 0 and 1 because of the semantics of eventfd(2).
		if (eventfd_write(event_fd, value) < 0) {
			die("cannot send information to %s preserved mount namespace", value_str);
		}
		// Exit, we're done.
		exit(0);
	}
	// This is back in the parent process.
	//
	// Enable a sanity timeout in case the read blocks for unbound amount of
	// time. This will ensure we will not hang around while holding the lock.
	// Next, read the value written by the child process.
	sc_enable_sanity_timeout();
	eventfd_t value = 0;
	if (eventfd_read(event_fd, &value) < 0) {
		die("cannot read from eventfd");
	}
	sc_disable_sanity_timeout();

	// Wait for the child process to exit and collect its exit status.
	errno = 0;
	int status = 0;
	if (waitpid(child, &status, 0) < 0) {
		die("cannot wait for the support process for mount namespace inspection");
	}
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0) {
		die("support process for mount namespace inspection exited abnormally");
	}
	// If the namespace is up-to-date then we are done.
	switch (value) {
	case SC_DISCARD_NO:
		debug("preserved mount is not stale, reusing");
		return 0;
	case SC_DISCARD_SHOULD:
		if (sc_is_mount_ns_in_use(inv->snap_instance)) {
			// Some processes are still using the namespace so we cannot discard it
			// as that would fracture the view that the set of processes inside
			// have on what is mounted.
			debug
			    ("preserved mount namespace is stale but occupied, reusing");
			return 0;
		}
		break;
	case SC_DISCARD_MUST:
		debug
		    ("preserved mount namespace is stale and base snap has changed, discarding");
		break;
	}
	sc_call_snap_discard_ns(snap_discard_ns_fd, inv->snap_instance);
	return EAGAIN;
}

static void helper_fork(struct sc_mount_ns *group,
			struct sc_apparmor *apparmor);
static void helper_main(struct sc_mount_ns *group, struct sc_apparmor *apparmor,
			pid_t parent);
static void helper_capture_ns(struct sc_mount_ns *group, pid_t parent);
static void helper_capture_per_user_ns(struct sc_mount_ns *group, pid_t parent);

int sc_join_preserved_ns(struct sc_mount_ns *group, struct sc_apparmor
			 *apparmor, const sc_invocation *inv,
			 int snap_discard_ns_fd)
{
	// Open the mount namespace file.
	char mnt_fname[PATH_MAX] = { 0 };
	sc_must_snprintf(mnt_fname, sizeof mnt_fname, "%s.mnt", group->name);
	int mnt_fd SC_CLEANUP(sc_cleanup_close) = -1;
	// NOTE: There is no O_EXCL here because the file can be around but
	// doesn't have to be a mounted namespace.
	mnt_fd = openat(group->dir_fd, mnt_fname,
			O_RDONLY | O_CLOEXEC | O_NOFOLLOW, 0600);
	if (mnt_fd < 0 && errno == ENOENT) {
		return ESRCH;
	}
	if (mnt_fd < 0) {
		die("cannot open preserved mount namespace %s", group->name);
	}
	// Check if we got an nsfs-based or procfs file or a regular file. This can
	// be reliably tested because nsfs has an unique filesystem type
	// NSFS_MAGIC.  On older kernels that don't support nsfs yet we can look
	// for PROC_SUPER_MAGIC instead.
	// We can just ensure that this is the case thanks to fstatfs.
	struct statfs ns_statfs_buf;
	if (fstatfs(mnt_fd, &ns_statfs_buf) < 0) {
		die("cannot inspect filesystem of preserved mount namespace file");
	}
	// Stat the mount namespace as well, this is later used to check if the
	// namespace is used by other processes if we are considering discarding a
	// stale namespace.
	struct stat ns_stat_buf;
	if (fstat(mnt_fd, &ns_stat_buf) < 0) {
		die("cannot inspect preserved mount namespace file");
	}
#ifndef NSFS_MAGIC
// Account for kernel headers old enough to not know about NSFS_MAGIC.
#define NSFS_MAGIC 0x6e736673
#endif
	if (ns_statfs_buf.f_type == NSFS_MAGIC
	    || ns_statfs_buf.f_type == PROC_SUPER_MAGIC) {

		// Inspect and perhaps discard the preserved mount namespace.
		if (sc_inspect_and_maybe_discard_stale_ns
		    (mnt_fd, inv, snap_discard_ns_fd) == EAGAIN) {
			return ESRCH;
		}
		// Move to the mount namespace of the snap we're trying to start.
		if (setns(mnt_fd, CLONE_NEWNS) < 0) {
			die("cannot join preserved mount namespace %s",
			    group->name);
		}
		debug("joined preserved mount namespace %s", group->name);
		return 0;
	}
	return ESRCH;
}

int sc_join_preserved_per_user_ns(struct sc_mount_ns *group,
				  const char *snap_name)
{
	uid_t uid = getuid();
	char mnt_fname[PATH_MAX] = { 0 };
	sc_must_snprintf(mnt_fname, sizeof mnt_fname, "%s.%d.mnt", group->name,
			 (int)uid);

	int mnt_fd SC_CLEANUP(sc_cleanup_close) = -1;
	mnt_fd = openat(group->dir_fd, mnt_fname,
			O_RDONLY | O_CLOEXEC | O_NOFOLLOW, 0600);
	if (mnt_fd < 0 && errno == ENOENT) {
		return ESRCH;
	}
	if (mnt_fd < 0) {
		die("cannot open preserved mount namespace %s", group->name);
	}
	struct statfs ns_statfs_buf;
	if (fstatfs(mnt_fd, &ns_statfs_buf) < 0) {
		die("cannot inspect filesystem of preserved mount namespace file");
	}
	struct stat ns_stat_buf;
	if (fstat(mnt_fd, &ns_stat_buf) < 0) {
		die("cannot inspect preserved mount namespace file");
	}
#ifndef NSFS_MAGIC
	/* Define NSFS_MAGIC for Ubuntu 14.04 and other older systems. */
#define NSFS_MAGIC 0x6e736673
#endif
	if (ns_statfs_buf.f_type == NSFS_MAGIC
	    || ns_statfs_buf.f_type == PROC_SUPER_MAGIC) {
		if (setns(mnt_fd, CLONE_NEWNS) < 0) {
			die("cannot join preserved per-user mount namespace %s",
			    group->name);
		}
		debug("joined preserved mount namespace %s", group->name);
		return 0;
	}
	return ESRCH;
}

static void setup_signals_for_helper(void)
{
	/* Ignore the SIGPIPE signal so that we get EPIPE on the read / write
	 * operations attempting to work with a closed pipe. This ensures that we
	 * are not killed by the default disposition (terminate) and can return a
	 * non-signal-death return code to the program invoking snap-confine. */
	if (signal(SIGPIPE, SIG_IGN) == SIG_ERR) {
		die("cannot install ignore handler for SIGPIPE");
	}
}

static void teardown_signals_for_helper(void)
{
	/* Undo operations done by setup_signals_for_helper. */
	if (signal(SIGPIPE, SIG_DFL) == SIG_ERR) {
		die("cannot restore default handler for SIGPIPE");
	}
}

static void helper_fork(struct sc_mount_ns *group, struct sc_apparmor *apparmor)
{
	// Create a pipe for sending commands to the helper process.
	if (pipe2(group->pipe_master, O_CLOEXEC | O_DIRECT) < 0) {
		die("cannot create pipes for commanding the helper process");
	}
	if (pipe2(group->pipe_helper, O_CLOEXEC | O_DIRECT) < 0) {
		die("cannot create pipes for responding to master process");
	}
	// Store the PID of the "parent" process. This done instead of calls to
	// getppid() because then we can reliably track the PID of the parent even
	// if the child process is re-parented.
	pid_t parent = getpid();

	// For rationale of forking see this:
	// https://lists.linuxfoundation.org/pipermail/containers/2013-August/033386.html
	pid_t pid = fork();
	if (pid < 0) {
		die("cannot fork helper process for mount namespace capture");
	}
	if (pid == 0) {
		/* helper */
		sc_cleanup_close(&group->pipe_master[1]);
		sc_cleanup_close(&group->pipe_helper[0]);
		helper_main(group, apparmor, parent);
	} else {
		setup_signals_for_helper();

		/* master */
		sc_cleanup_close(&group->pipe_master[0]);
		sc_cleanup_close(&group->pipe_helper[1]);

		// Glibc defines pid as a signed 32bit integer. There's no standard way to
		// print pid's portably so this is the best we can do.
		debug("forked support process %d", (int)pid);
		group->child = pid;
	}
}

static void helper_main(struct sc_mount_ns *group, struct sc_apparmor *apparmor,
			pid_t parent)
{
	// This is the child process which will capture the mount namespace.
	//
	// It will do so by bind-mounting the .mnt after the parent process calls
	// unshare() and finishes setting up the namespace completely. Change the
	// hat to a sub-profile that has limited permissions necessary to
	// accomplish the capture of the mount namespace.
	sc_maybe_aa_change_hat(apparmor, "mount-namespace-capture-helper", 0);
	// Configure the child to die as soon as the parent dies. In an odd
	// case where the parent is killed then we don't want to complete our
	// task or wait for anything.
	if (prctl(PR_SET_PDEATHSIG, SIGINT, 0, 0, 0) < 0) {
		die("cannot set parent process death notification signal to SIGINT");
	}
	// Check that parent process is still alive. If this is the case then we
	// can *almost* reliably rely on the PR_SET_PDEATHSIG signal to wake us up
	// from read(2) below. In the rare case that the PID numbers overflow and
	// the now-dead parent PID is recycled we will still hang forever on the
	// read from the pipe below.
	if (kill(parent, 0) < 0) {
		switch (errno) {
		case ESRCH:
			// When snap-confine executes it will fork a helper process. That
			// process establishes an elaborate dance to ensure both itself and
			// the parent are operating exactly as specified, so that no
			// processes are left behind for unbound amount of time. As a part
			// of that dance the child pings the parent to ensure it is still
			// alive after establishing a notification signal to be sent in
			// case the parent dies. This is a race avoidance mechanism, we set
			// up the notification and then check if the parent is alive by the
			// time we are done.
			//
			// In the case when the parent does go away we used to call
			// abort(). On some distributions this would trigger an unclean
			// process termination error report to be sent. One such example is
			// the Ubuntu error tracker. Since the parent process can be
			// legitimately interrupted and killed, this should not generate an
			// error report. As such, perform clean exit in this specific case.
			debug("parent process has terminated");
			exit(0);
		default:
			die("cannot confirm that parent process is alive");
			break;
		}
	}
	if (fchdir(group->dir_fd) < 0) {
		die("cannot move to directory with preserved namespaces");
	}
	int command = -1;
	int run = 1;
	while (run) {
		debug("helper process waiting for command");
		sc_enable_sanity_timeout();
		if (read(group->pipe_master[0], &command, sizeof command) < 0) {
			int saved_errno = errno;
			// This will ensure we get the correct error message
			// if there is a read error because the timeout
			// expired.
			sc_disable_sanity_timeout();
			errno = saved_errno;
			die("cannot read command from the pipe");
		}
		sc_disable_sanity_timeout();
		debug("helper process received command %d", command);
		switch (command) {
		case HELPER_CMD_EXIT:
			run = 0;
			break;
		case HELPER_CMD_CAPTURE_MOUNT_NS:
			helper_capture_ns(group, parent);
			break;
		case HELPER_CMD_CAPTURE_PER_USER_MOUNT_NS:
			helper_capture_per_user_ns(group, parent);
			break;
		}
		if (write(group->pipe_helper[1], &command, sizeof command) < 0) {
			die("cannot write ack");
		}
	}
	debug("helper process exiting");
	exit(0);
}

static void helper_capture_ns(struct sc_mount_ns *group, pid_t parent)
{
	char src[PATH_MAX] = { 0 };
	char dst[PATH_MAX] = { 0 };

	debug("capturing per-snap mount namespace");
	sc_must_snprintf(src, sizeof src, "/proc/%d/ns/mnt", (int)parent);
	sc_must_snprintf(dst, sizeof dst, "%s.mnt", group->name);

	/* Ensure the bind mount destination exists. */
	int fd = open(dst, O_CREAT | O_CLOEXEC | O_NOFOLLOW | O_RDONLY, 0600);
	if (fd < 0) {
		die("cannot create file %s", dst);
	}
	close(fd);

	if (mount(src, dst, NULL, MS_BIND, NULL) < 0) {
		die("cannot preserve mount namespace of process %d as %s",
		    (int)parent, dst);
	}
	debug("mount namespace of process %d preserved as %s",
	      (int)parent, dst);
}

static void helper_capture_per_user_ns(struct sc_mount_ns *group, pid_t parent)
{
	char src[PATH_MAX] = { 0 };
	char dst[PATH_MAX] = { 0 };
	uid_t uid = getuid();

	debug("capturing per-snap, per-user mount namespace");
	sc_must_snprintf(src, sizeof src, "/proc/%d/ns/mnt", (int)parent);
	sc_must_snprintf(dst, sizeof dst, "%s.%d.mnt", group->name, (int)uid);

	/* Ensure the bind mount destination exists. */
	int fd = open(dst, O_CREAT | O_CLOEXEC | O_NOFOLLOW | O_RDONLY, 0600);
	if (fd < 0) {
		die("cannot create file %s", dst);
	}
	close(fd);

	if (mount(src, dst, NULL, MS_BIND, NULL) < 0) {
		die("cannot preserve per-user mount namespace of process %d as %s", (int)parent, dst);
	}
	debug("per-user mount namespace of process %d preserved as %s",
	      (int)parent, dst);
}

static void sc_message_capture_helper(struct sc_mount_ns *group, int command_id)
{
	int ack;
	if (group->child == 0) {
		die("precondition failed: we don't have a helper process");
	}
	if (group->pipe_master[1] < 0) {
		die("precondition failed: we don't have a pipe");
	}
	if (group->pipe_helper[0] < 0) {
		die("precondition failed: we don't have a pipe");
	}
	debug("sending command %d to helper process (pid: %d)",
	      command_id, group->child);
	if (write(group->pipe_master[1], &command_id, sizeof command_id) < 0) {
		die("cannot send command %d to helper process", command_id);
	}
	debug("waiting for response from helper");
	int read_n = read(group->pipe_helper[0], &ack, sizeof ack);
	if (read_n < 0) {
		die("cannot receive ack from helper process");
	}
	if (read_n == 0) {
		die("unexpected eof from helper process");
	}
}

static void sc_wait_for_capture_helper(struct sc_mount_ns *group)
{
	if (group->child == 0) {
		die("precondition failed: we don't have a helper process");
	}
	debug("waiting for the helper process to exit");
	int status = 0;
	errno = 0;
	if (waitpid(group->child, &status, 0) < 0) {
		die("cannot wait for the helper process");
	}
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0) {
		die("helper process exited abnormally");
	}
	debug("helper process exited normally");
	group->child = 0;
	teardown_signals_for_helper();
}

void sc_fork_helper(struct sc_mount_ns *group, struct sc_apparmor *apparmor)
{
	helper_fork(group, apparmor);
}

void sc_preserve_populated_mount_ns(struct sc_mount_ns *group)
{
	sc_message_capture_helper(group, HELPER_CMD_CAPTURE_MOUNT_NS);
}

void sc_preserve_populated_per_user_mount_ns(struct sc_mount_ns *group)
{
	sc_message_capture_helper(group, HELPER_CMD_CAPTURE_PER_USER_MOUNT_NS);
}

void sc_wait_for_helper(struct sc_mount_ns *group)
{
	sc_message_capture_helper(group, HELPER_CMD_EXIT);
	sc_wait_for_capture_helper(group);
}

void sc_store_ns_info(const sc_invocation *inv)
{
	FILE *stream SC_CLEANUP(sc_cleanup_file) = NULL;
	char info_path[PATH_MAX] = { 0 };
	sc_must_snprintf(info_path, sizeof info_path,
			 "/run/snapd/ns/snap.%s.info", inv->snap_instance);
	int fd = -1;
	fd = open(info_path,
		  O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC | O_NOFOLLOW, 0644);
	if (fd < 0) {
		die("cannot open %s", info_path);
	}
	if (fchown(fd, 0, 0) < 0) {
		die("cannot chown %s to root.root", info_path);
	}
	// The stream now owns the file descriptor.
	stream = fdopen(fd, "w");
	if (stream == NULL) {
		die("cannot get stream from file descriptor");
	}
	fprintf(stream, "base-snap-name=%s\n", inv->orig_base_snap_name);
	if (ferror(stream) != 0) {
		die("I/O error when writing to %s", info_path);
	}
	if (fflush(stream) == EOF) {
		die("cannot flush %s", info_path);
	}
	debug("saved mount namespace meta-data to %s", info_path);
}

bool sc_is_mount_ns_in_use(const char *snap_instance)
{
	// perform an indirect check of whether the mount namespace is occupied,
	// with cgroups v1, each snap process is attached to a group under the
	// freezer controller, however with cgroups v2, we must check for any groups
	// tracking the snap
	bool occupied = false;
	if (sc_cgroup_is_v2()) {
		// cgroup v2 must consult the tracking groups
		occupied = sc_cgroup_v2_is_tracking_snap(snap_instance);
	} else {
		occupied = sc_cgroup_freezer_occupied(snap_instance);
	}
	return occupied;
}
