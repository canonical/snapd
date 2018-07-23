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
#include <linux/kdev_t.h>
#include <sched.h>
#include <signal.h>
#include <string.h>
#include <sys/eventfd.h>
#include <sys/file.h>
#include <sys/mount.h>
#include <sys/prctl.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/vfs.h>
#include <sys/wait.h>
#include <unistd.h>

#include "../libsnap-confine-private/cgroup-freezer-support.h"
#include "../libsnap-confine-private/classic.h"
#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/locking.h"
#include "../libsnap-confine-private/mountinfo.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"
#include "user-support.h"

/*!
 * The void directory.
 *
 * Snap confine moves to that directory in case it cannot retain the current
 * working directory across the pivot_root call.
 **/
#define SC_VOID_DIR "/var/lib/snapd/void"

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

/**
 * Name of the preserved mount namespace associated with SC_NS_DIR
 * and a given group identifier (typically SNAP_NAME).
 **/
#define SC_NS_MNT_FILE ".mnt"

/**
 * Read /proc/self/mountinfo and check if /run/snapd/ns is a private bind mount.
 *
 * We do this because /run/snapd/ns cannot be shared with any other peers as per:
 * https://www.kernel.org/doc/Documentation/filesystems/sharedsubtree.txt
 **/
static bool sc_is_ns_group_dir_private(void)
{
	struct sc_mountinfo *info SC_CLEANUP(sc_cleanup_mountinfo) = NULL;
	info = sc_parse_mountinfo(NULL);
	if (info == NULL) {
		die("cannot parse /proc/self/mountinfo");
	}
	struct sc_mountinfo_entry *entry = sc_first_mountinfo_entry(info);
	while (entry != NULL) {
		const char *mount_dir = entry->mount_dir;
		const char *optional_fields = entry->optional_fields;
		if (strcmp(mount_dir, sc_ns_dir) == 0
		    && strcmp(optional_fields, "") == 0) {
			// If /run/snapd/ns has no optional fields, we know it is mounted
			// private and there is nothing else to do.
			return true;
		}
		entry = sc_next_mountinfo_entry(entry);
	}
	return false;
}

void sc_reassociate_with_pid1_mount_ns(void)
{
	int init_mnt_fd SC_CLEANUP(sc_cleanup_close) = -1;
	int self_mnt_fd SC_CLEANUP(sc_cleanup_close) = -1;

	debug("checking if the current process shares mount namespace"
	      " with the init process");

	init_mnt_fd = open("/proc/1/ns/mnt",
			   O_RDONLY | O_CLOEXEC | O_NOFOLLOW | O_PATH);
	if (init_mnt_fd < 0) {
		die("cannot open mount namespace of the init process (O_PATH)");
	}
	self_mnt_fd = open("/proc/self/ns/mnt",
			   O_RDONLY | O_CLOEXEC | O_NOFOLLOW | O_PATH);
	if (self_mnt_fd < 0) {
		die("cannot open mount namespace of the current process (O_PATH)");
	}
	char init_buf[128] = { 0 };
	char self_buf[128] = { 0 };
	memset(init_buf, 0, sizeof init_buf);
	if (readlinkat(init_mnt_fd, "", init_buf, sizeof init_buf) < 0) {
		if (errno == ENOENT) {
			// According to namespaces(7) on a pre 3.8 kernel the namespace
			// files are hardlinks, not sylinks. If that happens readlinkat
			// fails with ENOENT. As a quick workaround for this special-case
			// functionality, just bail out and do nothing without raising an
			// error.
			return;
		}
		die("cannot perform readlinkat() on the mount namespace file "
		    "descriptor of the init process");
	}
	memset(self_buf, 0, sizeof self_buf);
	if (readlinkat(self_mnt_fd, "", self_buf, sizeof self_buf) < 0) {
		die("cannot perform readlinkat() on the mount namespace file "
		    "descriptor of the current process");
	}
	if (memcmp(init_buf, self_buf, sizeof init_buf) != 0) {
		debug("the current process does not share mount namespace with "
		      "the init process, re-association required");
		// NOTE: we cannot use O_NOFOLLOW here because that file will always be a
		// symbolic link. We actually want to open it this way.
		int init_mnt_fd_real SC_CLEANUP(sc_cleanup_close) = -1;
		init_mnt_fd_real = open("/proc/1/ns/mnt", O_RDONLY | O_CLOEXEC);
		if (init_mnt_fd_real < 0) {
			die("cannot open mount namespace of the init process");
		}
		if (setns(init_mnt_fd_real, CLONE_NEWNS) < 0) {
			die("cannot re-associate the mount namespace with the init process");
		}
	} else {
		debug("re-associating is not required");
	}
}

void sc_initialize_ns_groups(void)
{
	debug("creating namespace group directory %s", sc_ns_dir);
	if (sc_nonfatal_mkpath(sc_ns_dir, 0755) < 0) {
		die("cannot create namespace group directory %s", sc_ns_dir);
	}
	if (!sc_is_ns_group_dir_private()) {
		debug
		    ("bind mounting the namespace group directory over itself");
		if (mount(sc_ns_dir, sc_ns_dir, NULL, MS_BIND | MS_REC, NULL) <
		    0) {
			die("cannot bind mount namespace group directory over itself");
		}
		debug
		    ("making the namespace group directory mount point private");
		if (mount(NULL, sc_ns_dir, NULL, MS_PRIVATE, NULL) < 0) {
			die("cannot make the namespace group directory mount point private");
		}
	} else {
		debug
		    ("namespace group directory does not require intialization");
	}
}

struct sc_ns_group {
	// Name of the namespace group ($SNAP_NAME).
	char *name;
	// Descriptor to the namespace group control directory.  This descriptor is
	// opened with O_PATH|O_DIRECTORY so it's only used for openat() calls.
	int dir_fd;
	// Descriptor to an eventfd that is used to notify the child that it can
	// now complete its job and exit.
	int event_fd;
	// Identifier of the child process that is used during the one-time (per
	// group) initialization and capture process.
	pid_t child;
	// Flag set when this process created a fresh namespace should populate it.
	bool should_populate;
};

static struct sc_ns_group *sc_alloc_ns_group(void)
{
	struct sc_ns_group *group = calloc(1, sizeof *group);
	if (group == NULL) {
		die("cannot allocate memory for namespace group");
	}
	group->dir_fd = -1;
	group->event_fd = -1;
	// Redundant with calloc but some functions check for the non-zero value so
	// I'd like to keep this explicit in the code.
	group->child = 0;
	return group;
}

struct sc_ns_group *sc_open_ns_group(const char *group_name,
				     const unsigned flags)
{
	struct sc_ns_group *group = sc_alloc_ns_group();
	debug("opening namespace group directory %s", sc_ns_dir);
	group->dir_fd =
	    open(sc_ns_dir, O_DIRECTORY | O_PATH | O_CLOEXEC | O_NOFOLLOW);
	if (group->dir_fd < 0) {
		if (flags & SC_NS_FAIL_GRACEFULLY && errno == ENOENT) {
			free(group);
			return NULL;
		}
		die("cannot open directory for namespace group %s", group_name);
	}
	group->name = strdup(group_name);
	if (group->name == NULL) {
		die("cannot duplicate namespace group name %s", group_name);
	}
	return group;
}

void sc_close_ns_group(struct sc_ns_group *group)
{
	debug("releasing resources associated with namespace group %s",
	      group->name);
	sc_cleanup_close(&group->dir_fd);
	sc_cleanup_close(&group->event_fd);
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
	struct sc_mountinfo *mi SC_CLEANUP(sc_cleanup_mountinfo) = NULL;
	mi = sc_parse_mountinfo(NULL);
	if (mi == NULL) {
		die("cannot parse mountinfo of the current process");
	}
	bool found = false;
	for (struct sc_mountinfo_entry * mie =
	     sc_first_mountinfo_entry(mi); mie != NULL;
	     mie = sc_next_mountinfo_entry(mie)) {
		if (sc_streq(mie->mount_dir, base_squashfs_path)) {
			base_snap_dev = MKDEV(mie->dev_major, mie->dev_minor);
			debug("found base snap filesystem device %d:%d",
			      mie->dev_major, mie->dev_minor);
			// Don't break when found, we are interested in the last
			// entry as this is the "effective" one.
			found = true;
		}
	}
	if (!found) {
		die("cannot find device backing the base snap %s",
		    base_snap_name);
	}
	return base_snap_dev;
}

static bool should_discard_current_ns(dev_t base_snap_dev)
{
	// Inspect the namespace and check if we should discard it.
	//
	// The namespace may become "stale" when the rootfs is not the same
	// device we found above. This will happen whenever the base snap is
	// refreshed since the namespace was first created.
	struct sc_mountinfo_entry *mie;
	struct sc_mountinfo *mi SC_CLEANUP(sc_cleanup_mountinfo) = NULL;

	mi = sc_parse_mountinfo(NULL);
	if (mi == NULL) {
		die("cannot parse mountinfo of the current process");
	}
	for (mie = sc_first_mountinfo_entry(mi); mie != NULL;
	     mie = sc_next_mountinfo_entry(mie)) {
		if (!sc_streq(mie->mount_dir, "/")) {
			continue;
		}
		// NOTE: we want the initial rootfs just in case overmount
		// was used to do something weird. The initial rootfs was
		// set up by snap-confine and that is the one we want to
		// measure.
		debug("found root filesystem inside the mount namespace %d:%d",
		      mie->dev_major, mie->dev_minor);
		return base_snap_dev != MKDEV(mie->dev_major, mie->dev_minor);
	}
	die("cannot find mount entry of the root filesystem inside snap namespace");
}

enum sc_discard_vote {
	SC_DISCARD_NO = 1,
	SC_DISCARD_YES = 2,
};

// The namespace may be stale. To check this we must actually switch into it
// but then we use up our setns call (the kernel misbehaves if we setns twice).
// To work around this we'll fork a child and use it to probe. The child will
// inspect the namespace and send information back via eventfd and then exit
// unconditionally.
static int sc_inspect_and_maybe_discard_stale_ns(int mnt_fd,
						 const char *snap_name,
						 const char *base_snap_name)
{
	char base_snap_rev[PATH_MAX] = { 0 };
	char fname[PATH_MAX] = { 0 };
	char mnt_fname[PATH_MAX] = { 0 };
	dev_t base_snap_dev;
	int event_fd SC_CLEANUP(sc_cleanup_close) = -1;

	// Read the revision of the base snap by looking at the current symlink.
	sc_must_snprintf(fname, sizeof fname, "%s/%s/current",
			 SNAP_MOUNT_DIR, base_snap_name);
	if (readlink(fname, base_snap_rev, sizeof base_snap_rev) < 0) {
		die("cannot read revision of base snap %s", fname);
	}
	if (base_snap_rev[sizeof base_snap_rev - 1] != '\0') {
		die("cannot use symbolic link %s - value is too long", fname);
	}
	// Find the device that is backing the current revision of the base snap.
	base_snap_dev = find_base_snap_device(base_snap_name, base_snap_rev);

	// Check if we are running in normal mode with pivot root. Do this here
	// because once on the inside of the transformed mount namespace we can no
	// longer tell.
	bool is_normal_mode =
	    sc_should_use_normal_mode(sc_classify_distro(), base_snap_name);

	// Store the PID of this process. This is done instead of calls to
	// getppid() below because then we can reliably track the PID of the
	// parent even if the child process is re-parented.
	pid_t parent = getpid();

	// Create an eventfd for the communication with the child.
	event_fd = eventfd(0, EFD_CLOEXEC);
	if (event_fd < 0) {
		die("cannot create eventfd for communication with inspection process");
	}
	// Fork a child, it will do the inspection for us.
	pid_t child = fork();
	if (child < 0) {
		die("cannot fork support process for namespace inspection");
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
		debug("ensuring that parent process is still alive");
		if (kill(parent, 0) < 0) {
			switch (errno) {
			case ESRCH:
				debug("parent process has already terminated");
				abort();
			default:
				die("cannot ensure that parent process is still alive");
				break;
			}
		}

		debug("joining the namespace that we are about to probe");
		// Move to the mount namespace of the snap we're trying to inspect.
		if (setns(mnt_fd, CLONE_NEWNS) < 0) {
			die("cannot join the mount namespace in order to inspect it");
		}
		// Check if the namespace needs to be discarded.
		//
		// TODO: enable this for core distributions. This is complex because on
		// core the rootfs is mounted in initrd and is _not_ changed (no
		// pivot_root) and the base snap is again mounted (2nd time) by
		// systemd. This makes us end up in a situation where the outer base
		// snap will never match the rootfs inside the mount namespace.
		bool should_discard =
		    is_normal_mode ? should_discard_current_ns(base_snap_dev) :
		    false;

		// Send this back to the parent: 2 - discard, 1 - keep.
		// Note that we cannot just use 0 and 1 because of the semantics of eventfd(2).
		debug
		    ("sending information about the state of the mount namespace (%s)",
		     should_discard ? "discard" : "keep");
		if (eventfd_write
		    (event_fd,
		     should_discard ? SC_DISCARD_YES : SC_DISCARD_NO) < 0) {
			die("cannot send information about the state of the mount namespace");
		}
		// Exit, we're done.
		debug
		    ("support process for mount namespace inspection is about to finish");
		exit(0);
	}
	// This is back in the parent process.
	//
	// Enable a sanity timeout in case the read blocks for unbound amount of
	// time. This will ensure we will not hang around while holding the lock.
	// Next, read the value written by the child process.
	sc_enable_sanity_timeout();
	eventfd_t value = 0;
	debug("receiving information about the state of the mount namespace");
	if (eventfd_read(event_fd, &value) < 0) {
		die("cannot receive information about the state of the mount namespace");
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
	if (value == SC_DISCARD_NO) {
		debug("the mount namespace is up-to-date and can be reused");
		return 0;
	}
	// The namespace is stale, let's check if we can discard it.
	debug("the mount namespace is stale and should be discarded");
	if (sc_cgroup_freezer_occupied(snap_name)) {
		// Some processes are still using the namespace so we cannot discard it
		// as that would fracture the view that the set of processes inside
		// have on what is mounted.
		return 0;
	}
	// The namespace is both stale and empty. We can discard it now.
	debug("discarding stale and empty mount namespace");
	sc_must_snprintf(mnt_fname, sizeof mnt_fname,
			 "%s/%s%s", sc_ns_dir, snap_name, SC_NS_MNT_FILE);

	// Use MNT_DETACH as otherwise we get EBUSY.
	if (umount2(mnt_fname, MNT_DETACH | UMOUNT_NOFOLLOW) < 0) {
		die("cannot umount stale mount namespace %s", mnt_fname);
	}
	debug("stale mount namespace discarded");
	return EAGAIN;
}

int sc_create_or_join_ns_group(struct sc_ns_group *group,
			       struct sc_apparmor *apparmor,
			       const char *base_snap_name,
			       const char *snap_name)
{
	// Open the mount namespace file.
	char mnt_fname[PATH_MAX] = { 0 };
	sc_must_snprintf(mnt_fname, sizeof mnt_fname, "%s%s", group->name,
			 SC_NS_MNT_FILE);
	int mnt_fd SC_CLEANUP(sc_cleanup_close) = -1;
	// NOTE: There is no O_EXCL here because the file can be around but
	// doesn't have to be a mounted namespace.
	//
	// If the mounted namespace is discarded with
	// sc_discard_preserved_ns_group() it will revert to a regular file.  If
	// snap-confine is killed for whatever reason after the file is created but
	// before the file is bind-mounted it will also be a regular file.
	mnt_fd = openat(group->dir_fd, mnt_fname,
			O_CREAT | O_RDONLY | O_CLOEXEC | O_NOFOLLOW, 0600);
	if (mnt_fd < 0) {
		die("cannot open mount namespace file for namespace group %s",
		    group->name);
	}
	// Check if we got an nsfs-based or procfs file or a regular file. This can
	// be reliably tested because nsfs has an unique filesystem type
	// NSFS_MAGIC.  On older kernels that don't support nsfs yet we can look
	// for PROC_SUPER_MAGIC instead. 
	// We can just ensure that this is the case thanks to fstatfs.
	struct statfs ns_statfs_buf;
	if (fstatfs(mnt_fd, &ns_statfs_buf) < 0) {
		die("cannot perform fstatfs() on the mount namespace file descriptor");
	}
	// Stat the mount namespace as well, this is later used to check if the
	// namespace is used by other processes if we are considering discarding a
	// stale namespace.
	struct stat ns_stat_buf;
	if (fstat(mnt_fd, &ns_stat_buf) < 0) {
		die("cannot perform fstat() on the mount namespace file descriptor");
	}
#ifndef NSFS_MAGIC
// Account for kernel headers old enough to not know about NSFS_MAGIC.
#define NSFS_MAGIC 0x6e736673
#endif
	if (ns_statfs_buf.f_type == NSFS_MAGIC
	    || ns_statfs_buf.f_type == PROC_SUPER_MAGIC) {

		// Inspect and perhaps discard the preserved mount namespace.
		if (sc_inspect_and_maybe_discard_stale_ns
		    (mnt_fd, snap_name, base_snap_name) == EAGAIN) {
			return EAGAIN;
		}
		// Remember the vanilla working directory so that we may attempt to restore it later.
		char *vanilla_cwd SC_CLEANUP(sc_cleanup_string) = NULL;
		vanilla_cwd = get_current_dir_name();
		if (vanilla_cwd == NULL) {
			die("cannot get the current working directory");
		}
		// Move to the mount namespace of the snap we're trying to start.
		debug
		    ("attempting to re-associate the mount namespace with the namespace group %s",
		     group->name);
		if (setns(mnt_fd, CLONE_NEWNS) < 0) {
			die("cannot re-associate the mount namespace with namespace group %s", group->name);
		}
		debug
		    ("successfully re-associated the mount namespace with the namespace group %s",
		     group->name);

		// Try to re-locate back to vanilla working directory. This can fail
		// because that directory is no longer present.
		if (chdir(vanilla_cwd) != 0) {
			debug
			    ("cannot remain in %s, moving to the void directory",
			     vanilla_cwd);
			if (chdir(SC_VOID_DIR) != 0) {
				die("cannot change directory to %s",
				    SC_VOID_DIR);
			}
			debug("successfully moved to %s", SC_VOID_DIR);
		}
		return 0;
	}
	debug("initializing new namespace group %s", group->name);
	// Create a new namespace and ask the caller to populate it.
	// For rationale of forking see this:
	// https://lists.linuxfoundation.org/pipermail/containers/2013-August/033386.html
	//
	// The eventfd created here is used to synchronize the child and the parent
	// processes. It effectively tells the child to perform the capture
	// operation.
	group->event_fd = eventfd(0, EFD_CLOEXEC);
	if (group->event_fd < 0) {
		die("cannot create eventfd for mount namespace capture");
	}
	debug("forking support process for mount namespace capture");
	// Store the PID of the "parent" process. This done instead of calls to
	// getppid() because then we can reliably track the PID of the parent even
	// if the child process is re-parented.
	pid_t parent = getpid();
	// Glibc defines pid as a signed 32bit integer. There's no standard way to
	// print pid's portably so this is the best we can do.
	pid_t pid = fork();
	debug("forked support process has pid %d", (int)pid);
	if (pid < 0) {
		die("cannot fork support process for mount namespace capture");
	}
	if (pid == 0) {
		// This is the child process which will capture the mount namespace.
		//
		// It will do so by bind-mounting the SC_NS_MNT_FILE after the parent
		// process calls unshare() and finishes setting up the namespace
		// completely.
		// Change the hat to a sub-profile that has limited permissions
		// necessary to accomplish the capture of the mount namespace.
		debug
		    ("changing apparmor hat of the support process for mount namespace capture");
		sc_maybe_aa_change_hat(apparmor,
				       "mount-namespace-capture-helper", 0);
		// Configure the child to die as soon as the parent dies. In an odd
		// case where the parent is killed then we don't want to complete our
		// task or wait for anything.
		if (prctl(PR_SET_PDEATHSIG, SIGINT, 0, 0, 0) < 0) {
			die("cannot set parent process death notification signal to SIGINT");
		}
		// Check that parent process is still alive. If this is the case then
		// we can *almost* reliably rely on the PR_SET_PDEATHSIG signal to wake
		// us up from eventfd_read() below. In the rare case that the PID numbers
		// overflow and the now-dead parent PID is recycled we will still hang
		// forever on the read from eventfd below.
		debug("ensuring that parent process is still alive");
		if (kill(parent, 0) < 0) {
			switch (errno) {
			case ESRCH:
				debug("parent process has already terminated");
				abort();
			default:
				die("cannot ensure that parent process is still alive");
				break;
			}
		}
		if (fchdir(group->dir_fd) < 0) {
			die("cannot move process for mount namespace capture to namespace group directory");
		}
		debug
		    ("waiting for a eventfd data from the parent process to continue");
		eventfd_t value = 0;
		sc_enable_sanity_timeout();
		if (eventfd_read(group->event_fd, &value) < 0) {
			die("cannot read expected data from eventfd");
		}
		sc_disable_sanity_timeout();
		debug
		    ("capturing mount namespace of process %d in namespace group %s",
		     (int)parent, group->name);
		char src[PATH_MAX] = { 0 };
		char dst[PATH_MAX] = { 0 };
		sc_must_snprintf(src, sizeof src, "/proc/%d/ns/mnt",
				 (int)parent);
		sc_must_snprintf(dst, sizeof dst, "%s%s", group->name,
				 SC_NS_MNT_FILE);
		if (mount(src, dst, NULL, MS_BIND, NULL) < 0) {
			die("cannot bind-mount the mount namespace file %s -> %s", src, dst);
		}
		debug
		    ("successfully captured mount namespace in namespace group %s",
		     group->name);
		exit(0);
	} else {
		group->child = pid;
		// Unshare the mount namespace and set a flag instructing the caller that 
		// the namespace is pristine and needs to be populated now.
		debug("unsharing the mount namespace");
		if (unshare(CLONE_NEWNS) < 0) {
			die("cannot unshare the mount namespace");
		}
		group->should_populate = true;
	}
	return 0;
}

bool sc_should_populate_ns_group(struct sc_ns_group * group)
{
	return group->should_populate;
}

void sc_preserve_populated_ns_group(struct sc_ns_group *group)
{
	if (group->child == 0) {
		die("precondition failed: we don't have a support process for mount namespace capture");
	}
	if (group->event_fd < 0) {
		die("precondition failed: we don't have an eventfd for mount namespace capture");
	}
	debug
	    ("asking support process for mount namespace capture (pid: %d) to perform the capture",
	     group->child);
	if (eventfd_write(group->event_fd, 1) < 0) {
		die("cannot write eventfd");
	}
	debug
	    ("waiting for the support process for mount namespace capture to exit");
	int status = 0;
	errno = 0;
	if (waitpid(group->child, &status, 0) < 0) {
		die("cannot wait for the support process for mount namespace capture");
	}
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0) {
		die("support process for mount namespace capture exited abnormally");
	}
	debug("support process for mount namespace capture exited normally");
	group->child = 0;
}

void sc_discard_preserved_ns_group(struct sc_ns_group *group)
{
	// Remember the current working directory
	int old_dir_fd SC_CLEANUP(sc_cleanup_close) = -1;
	old_dir_fd = open(".", O_PATH | O_DIRECTORY | O_CLOEXEC);
	if (old_dir_fd < 0) {
		die("cannot open current directory");
	}
	// Move to the mount namespace directory (/run/snapd/ns)
	if (fchdir(group->dir_fd) < 0) {
		die("cannot move to namespace group directory");
	}
	// Unmount ${group_name}.mnt which holds the preserved namespace
	char mnt_fname[PATH_MAX] = { 0 };
	sc_must_snprintf(mnt_fname, sizeof mnt_fname, "%s%s", group->name,
			 SC_NS_MNT_FILE);
	debug("unmounting preserved mount namespace file %s", mnt_fname);
	if (umount2(mnt_fname, UMOUNT_NOFOLLOW) < 0) {
		switch (errno) {
		case EINVAL:
			// EINVAL is returned when there's nothing to unmount (no bind-mount).
			// Instead of checking for this explicitly (which is always racy) we
			// just unmount and check the return code.
			break;
		case ENOENT:
			// We may be asked to discard a namespace that doesn't yet
			// exist (even the mount point may be absent). We just
			// ignore that error and return gracefully.
			break;
		default:
			die("cannot unmount preserved mount namespace file %s",
			    mnt_fname);
			break;
		}
	}
	// Get back to the original directory
	if (fchdir(old_dir_fd) < 0) {
		die("cannot move back to original directory");
	}
}
