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
#include <sched.h>
#include <signal.h>
#include <string.h>
#include <sys/eventfd.h>
#include <sys/file.h>
#include <sys/mount.h>
#include <sys/prctl.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>
#ifdef HAVE_APPARMOR
#include <sys/apparmor.h>
#endif				// ifdef HAVE_APPARMOR

#include "utils.h"
#include "user-support.h"
#include "mountinfo.h"
#include "cleanup-funcs.h"

/**
 * Directory where snap-confine keeps namespace files.
 **/
#define SC_NS_DIR "/run/snapd/ns"

/**
 * Effective value of SC_NS_DIR.
 *
 * This is only altered for testing.
 **/
static const char *sc_ns_dir = SC_NS_DIR;

/**
 * Name of the lock file associated with SC_NS_DIR.
 * and a given group identifier (typically SNAP_NAME).
 **/
#define SC_NS_LOCK_FILE ".lock"

/**
 * Name of the preserved mount namespace associated with SC_NS_DIR
 * and a given group identifier (typically SNAP_NAME).
 **/
#define SC_NS_MNT_FILE ".mnt"

// Read /proc/self/mountinfo and check if /run/snapd/ns is a private bind mount.
//
// That is, it cannot be shared with any other peer as defined by kernel
// documentation listed here:
// https://www.kernel.org/doc/Documentation/filesystems/sharedsubtree.txt
static bool sc_is_ns_group_dir_private()
{
	struct mountinfo *info
	    __attribute__ ((cleanup(cleanup_mountinfo))) = NULL;
	info = parse_mountinfo(NULL);
	if (info == NULL) {
		die("cannot parse /proc/self/mountinfo");
	}
	struct mountinfo_entry *entry = first_mountinfo_entry(info);
	while (entry != NULL) {
		const char *mount_dir = mountinfo_entry_mount_dir(entry);
		const char *optional_fields =
		    mountinfo_entry_optional_fields(entry);
		if (strcmp(mount_dir, sc_ns_dir) == 0
		    && strcmp(optional_fields, "") == 0) {
			// If /run/snapd/ns has no optional fields, we know it is mounted
			// private and there is nothing else to do.
			return true;
		}
		entry = next_mountinfo_entry(entry);
	}
	return false;
}

void sc_initialize_ns_groups()
{
	int dir_fd __attribute__ ((cleanup(sc_cleanup_close))) = -1;
	debug("creating namespace group directory %s", sc_ns_dir);
	mkpath(sc_ns_dir);
	debug("opening namespace group directory %s", sc_ns_dir);
	dir_fd = open(sc_ns_dir, O_DIRECTORY | O_PATH | O_CLOEXEC | O_NOFOLLOW);
	if (dir_fd < 0) {
		die("cannot open namespace group directory");
	}
	debug("opening lock file for group directory");
	int lock_fd __attribute__ ((cleanup(sc_cleanup_close))) = -1;
	lock_fd = openat(dir_fd,
			 SC_NS_LOCK_FILE,
			 O_CREAT | O_RDWR | O_CLOEXEC | O_NOFOLLOW, 0600);
	if (lock_fd < 0) {
		die("cannot open lock file for namespace group directory");
	}
	debug("locking the namespace group directory");
	if (flock(lock_fd, LOCK_EX) < 0) {
		die("cannot acquire exclusive lock for namespace group directory");
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
	debug("unlocking the namespace group directory");
	if (flock(lock_fd, LOCK_UN) < 0) {
		die("cannot release lock for namespace control directory");
	}
}

struct sc_ns_group {
	// Name of the namespace group ($SNAP_NAME).
	char *name;
	// Descriptor to the namespace group control directory.  This descriptor is
	// opened with O_PATH|O_DIRECTORY so it's only used for openat() calls.
	int dir_fd;
	// Descriptor to a namespace-specific lock file (i.e. $SNAP_NAME.lock).
	int lock_fd;
	// Descriptor to an eventfd that is used to notify the child that it can
	// now complete its job and exit.
	int event_fd;
	// Identifier of the child process that is used during the one-time (per
	// group) initialization and capture process.
	pid_t child;
	// Flag set when this process created a fresh namespace should populate it.
	bool should_populate;
};

static struct sc_ns_group *sc_alloc_ns_group()
{
	struct sc_ns_group *group = calloc(1, sizeof *group);
	if (group == NULL) {
		die("cannot allocate memory for namespace group");
	}
	group->dir_fd = -1;
	group->lock_fd = -1;
	group->event_fd = -1;
	// Redundant with calloc but some functions check for the non-zero value so
	// I'd like to keep this explicit in the code.
	group->child = 0;
	return group;
}

struct sc_ns_group *sc_open_ns_group(const char *group_name)
{
	struct sc_ns_group *group = sc_alloc_ns_group();
	debug("opening namespace group directory %s", sc_ns_dir);
	group->dir_fd =
	    open(sc_ns_dir, O_DIRECTORY | O_PATH | O_CLOEXEC | O_NOFOLLOW);
	if (group->dir_fd < 0) {
		die("cannot open directory for namespace group %s", group_name);
	}
	char lock_fname[PATH_MAX];
	must_snprintf(lock_fname, sizeof lock_fname, "%s%s", group_name,
		      SC_NS_LOCK_FILE);
	debug("opening lock file for namespace group %s", group_name);
	group->lock_fd =
	    openat(group->dir_fd, lock_fname,
		   O_CREAT | O_RDWR | O_CLOEXEC | O_NOFOLLOW, 0600);
	if (group->lock_fd < 0) {
		die("cannot open lock file for namespace group %s", group_name);
	}
	group->name = strdup(group_name);
	if (group->name == NULL) {
		die("cannot duplicate namespace group name %s", group_name);
	}
	return group;
}

void sc_close_ns_group(struct sc_ns_group *group)
{
	debug("releasing resources associated wih namespace group %s",
	      group->name);
	close(group->dir_fd);
	close(group->lock_fd);
	close(group->event_fd);
	free(group->name);
	free(group);
}

void sc_lock_ns_mutex(struct sc_ns_group *group)
{
	if (group->lock_fd < 0) {
		die("precondition failed: we don't have an open file descriptor for the mutex file");
	}
	debug("acquiring exclusive lock for namespace group %s", group->name);
	if (flock(group->lock_fd, LOCK_EX) < 0) {
		die("cannot acquire exclusive lock for namespace group %s",
		    group->name);
	}
	debug("acquired exclusive lock for namespace group %s", group->name);
}

void sc_unlock_ns_mutex(struct sc_ns_group *group)
{
	if (group->lock_fd < 0) {
		die("precondition failed: we don't have an open file descriptor for the mutex file");
	}
	debug("releasing lock for namespace group %s", group->name);
	if (flock(group->lock_fd, LOCK_UN) < 0) {
		die("cannot release lock for namespace group %s", group->name);
	}
	debug("released lock for namespace group %s", group->name);
}

void sc_create_or_join_ns_group(struct sc_ns_group *group)
{
	// Open the mount namespace file.
	char mnt_fname[PATH_MAX];
	must_snprintf(mnt_fname, sizeof mnt_fname, "%s%s", group->name,
		      SC_NS_MNT_FILE);
	int mnt_fd __attribute__ ((cleanup(sc_cleanup_close))) = -1;
	// NOTE: There is no O_EXCL here because the file can be around but
	// doesn't have to be a mounted namespace.
	//
	// If the mounted namespace is discarded with
	// sc_discard_preserved_ns_group() it will revert to a regular file.  If
	// snap-confine is killed for whatever reason after the file is created but
	// before the file is bind-mounted it will also be a regular file.
	//
	// The code below handles this by trying to join the namespace with setns()
	// and handling both the successful and the unsuccessful paths.
	mnt_fd =
	    openat(group->dir_fd, mnt_fname,
		   O_CREAT | O_RDONLY | O_CLOEXEC | O_NOFOLLOW, 0600);
	if (mnt_fd < 0) {
		die("cannot open mount namespace file for namespace group %s",
		    group->name);
	}
	// attempt to join an existing group
	debug
	    ("attempting to re-associate the mount namespace with the namespace group %s",
	     group->name);
	if (setns(mnt_fd, CLONE_NEWNS) == 0) {
		debug
		    ("successfully re-associated the mount namespace with the namespace group %s",
		     group->name);
		return;
	}
	// Anything but EINVAL is an unexpected error.
	//
	// EINVAL is simply a sign that the file we've opened is not a valid
	// namespace file descriptor. One potential case where this can happen is
	// when another snap-confine tried to initialize the namespace but was
	// killed before it managed to complete the process.
	if (errno != EINVAL) {
		die("cannot re-associate the mount namespace with namespace group %s", group->name);
	}
	debug
	    ("cannot re-associate the mount namespace with namespace group %s, falling back to initialization",
	     group->name);
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
	pid_t pid = fork();
	debug("forked support process has pid %d", (int)pid);
	if (pid == -1) {
		die("cannot fork support process for mount namespace capture");
	}
	if (pid == 0) {
		// This is the child process which will capture the mount namespace.
		//
		// It will do so by bind-mounting the SC_NS_MNT_FILE after the parent
		// process calls unshare() and finishes setting up the namespace
		// completely.
#ifdef HAVE_APPARMOR
		// Change the hat to a sub-profile that has limited permissions
		// necessary to accomplish the capture of the mount namespace.
		debug
		    ("changing apparmor hat of the support process for mount namespace capture");
		if (aa_change_hat("mount-namespace-capture-helper", 0) < 0) {
			die("cannot change apparmor hat of the support process for mount namespace capture");
		}
#endif
		// Configure the child to die as soon as the parent dies. In an odd
		// case where the parent is killed then we don't want to complete our
		// task or wait for anything.
		if (prctl(PR_SET_PDEATHSIG, SIGINT, 0, 0, 0) < 0) {
			die("cannot set parent process death notification signal to SIGINT");
		}
		if (fchdir(group->dir_fd) < 0) {
			die("cannot move process for mount namespace capture to namespace group directory");
		}
		debug
		    ("waiting for a eventfd data from the parent process to continue");
		eventfd_t value = 0;
		if (eventfd_read(group->event_fd, &value) < 0) {
			die("cannot read expected data from eventfd");
		}
		pid_t parent = getppid();
		debug
		    ("capturing mount namespace of process %d in namespace group %s",
		     (int)parent, group->name);
		char src[PATH_MAX];
		char dst[PATH_MAX];
		must_snprintf(src, sizeof src, "/proc/%d/ns/mnt", (int)parent);
		must_snprintf(dst, sizeof dst, "%s%s", group->name,
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
}

bool sc_should_populate_ns_group(struct sc_ns_group *group)
{
	return group->should_populate;
}

void sc_preserve_ns_group(struct sc_ns_group *group)
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
	int old_dir_fd __attribute__ ((cleanup(sc_cleanup_close))) = -1;
	old_dir_fd = open(".", O_PATH | O_DIRECTORY | O_CLOEXEC);
	if (old_dir_fd < 0) {
		die("cannot open current directory");
	}
	// Move to the mount namespace directory (/run/snapd/ns)
	if (fchdir(group->dir_fd) < 0) {
		die("cannot move to namespace group directory");
	}
	// Unmount ${group_name}.mnt which holds the preserved namespace
	char mnt_fname[PATH_MAX];
	must_snprintf(mnt_fname, sizeof mnt_fname, "%s%s", group->name,
		      SC_NS_MNT_FILE);
	debug("unmounting preserved mount namespace file %s", mnt_fname);
	if (umount2(mnt_fname, UMOUNT_NOFOLLOW) < 0) {
		// EINVAL is returned when there's nothing to unmount (no bind-mount).
		// Instead of checking for this explicitly (which is always racy) we
		// just unmount and check the return code.
		if (errno != EINVAL) {
			die("cannot unmount preserved mount namespace file %s",
			    mnt_fname);
		}
	}
	// Get back to the original directory
	if (fchdir(old_dir_fd) < 0) {
		die("cannot move back to original directory");
	}
}
