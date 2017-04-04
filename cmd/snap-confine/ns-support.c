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
#include <sys/types.h>
#include <sys/vfs.h>
#include <sys/wait.h>
#include <unistd.h>

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/mountinfo.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"
#include "user-support.h"

/**
 * Flag indicating that a sanity timeout has expired.
 **/
static volatile sig_atomic_t sanity_timeout_expired = 0;

/**
 * Signal handler for SIGALRM that sets sanity_timeout_expired flag to 1.
 **/
static void sc_SIGALRM_handler(int signum)
{
	sanity_timeout_expired = 1;
}

/**
 * Enable a sanity-check timeout.
 *
 * The timeout is based on good-old alarm(2) and is intended to break a
 * suspended system call, such as flock, after a few seconds. The built-in
 * timeout is primed for three seconds. After that any sleeping system calls
 * are interrupted and a flag is set.
 *
 * The call should be paired with sc_disable_sanity_check_timeout() that
 * disables the alarm and acts on the flag, aborting the process if the timeout
 * gets exceeded.
 **/
static void sc_enable_sanity_timeout()
{
	sanity_timeout_expired = 0;
	struct sigaction act = {.sa_handler = sc_SIGALRM_handler };
	if (sigemptyset(&act.sa_mask) < 0) {
		die("cannot initialize POSIX signal set");
	}
	// NOTE: we are using sigaction so that we can explicitly control signal
	// flags and *not* pass the SA_RESTART flag. The intent is so that any
	// system call we may be sleeping on to get interrupted.
	act.sa_flags = 0;
	if (sigaction(SIGALRM, &act, NULL) < 0) {
		die("cannot install signal handler for SIGALRM");
	}
	alarm(3);
	debug("sanity timeout initialized and set for three seconds");
}

/**
 * Disable sanity-check timeout and abort the process if it expired.
 *
 * This call has to be paired with sc_enable_sanity_timeout(), see the function
 * description for more details.
 **/
static void sc_disable_sanity_timeout()
{
	if (sanity_timeout_expired) {
		die("sanity timeout expired");
	}
	alarm(0);
	struct sigaction act = {.sa_handler = SIG_DFL };
	if (sigemptyset(&act.sa_mask) < 0) {
		die("cannot initialize POSIX signal set");
	}
	if (sigaction(SIGALRM, &act, NULL) < 0) {
		die("cannot uninstall signal handler for SIGALRM");
	}
	debug("sanity timeout reset and disabled");
}

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
 * Name of the lock file associated with SC_NS_DIR.
 * and a given group identifier (typically SNAP_NAME).
 **/
#define SC_NS_LOCK_FILE ".lock"

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
static bool sc_is_ns_group_dir_private()
{
	struct sc_mountinfo *info
	    __attribute__ ((cleanup(sc_cleanup_mountinfo))) = NULL;
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

void sc_reassociate_with_pid1_mount_ns()
{
	int init_mnt_fd __attribute__ ((cleanup(sc_cleanup_close))) = -1;
	int self_mnt_fd __attribute__ ((cleanup(sc_cleanup_close))) = -1;

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
	char init_buf[128], self_buf[128];
	memset(init_buf, 0, sizeof init_buf);
	if (readlinkat(init_mnt_fd, "", init_buf, sizeof init_buf) < 0) {
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
		int init_mnt_fd_real
		    __attribute__ ((cleanup(sc_cleanup_close))) = -1;
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

void sc_initialize_ns_groups()
{
	debug("creating namespace group directory %s", sc_ns_dir);
	if (sc_nonfatal_mkpath(sc_ns_dir, 0755) < 0) {
		die("cannot create namespace group directory %s", sc_ns_dir);
	}
	debug("opening namespace group directory %s", sc_ns_dir);
	int dir_fd __attribute__ ((cleanup(sc_cleanup_close))) = -1;
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
	sc_enable_sanity_timeout();
	if (flock(lock_fd, LOCK_EX) < 0) {
		die("cannot acquire exclusive lock for namespace group directory");
	}
	sc_disable_sanity_timeout();
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
	char lock_fname[PATH_MAX];
	sc_must_snprintf(lock_fname, sizeof lock_fname, "%s%s", group_name,
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
	debug("releasing resources associated with namespace group %s",
	      group->name);
	sc_cleanup_close(&group->dir_fd);
	sc_cleanup_close(&group->lock_fd);
	sc_cleanup_close(&group->event_fd);
	free(group->name);
	free(group);
}

void sc_lock_ns_mutex(struct sc_ns_group *group)
{
	if (group->lock_fd < 0) {
		die("precondition failed: we don't have an open file descriptor for the mutex file");
	}
	debug("acquiring exclusive lock for namespace group %s", group->name);
	sc_enable_sanity_timeout();
	if (flock(group->lock_fd, LOCK_EX) < 0) {
		die("cannot acquire exclusive lock for namespace group %s",
		    group->name);
	}
	sc_disable_sanity_timeout();
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

void sc_create_or_join_ns_group(struct sc_ns_group *group,
				struct sc_apparmor *apparmor)
{
	// Open the mount namespace file.
	char mnt_fname[PATH_MAX];
	sc_must_snprintf(mnt_fname, sizeof mnt_fname, "%s%s", group->name,
			 SC_NS_MNT_FILE);
	int mnt_fd __attribute__ ((cleanup(sc_cleanup_close))) = -1;
	// NOTE: There is no O_EXCL here because the file can be around but
	// doesn't have to be a mounted namespace.
	//
	// If the mounted namespace is discarded with
	// sc_discard_preserved_ns_group() it will revert to a regular file.  If
	// snap-confine is killed for whatever reason after the file is created but
	// before the file is bind-mounted it will also be a regular file.
	mnt_fd =
	    openat(group->dir_fd, mnt_fname,
		   O_CREAT | O_RDONLY | O_CLOEXEC | O_NOFOLLOW, 0600);
	if (mnt_fd < 0) {
		die("cannot open mount namespace file for namespace group %s",
		    group->name);
	}
	// Check if we got an nsfs-based file or a regular file. This can be
	// reliably tested because nsfs has an unique filesystem type NSFS_MAGIC.
	// On older kernels that don't support nsfs yet we can look for
	// PROC_SUPER_MAGIC instead.
	// We can just ensure that this is the case thanks to fstatfs.
	struct statfs buf;
	if (fstatfs(mnt_fd, &buf) < 0) {
		die("cannot perform fstatfs() on an mount namespace file descriptor");
	}
#ifndef NSFS_MAGIC
// Account for kernel headers old enough to not know about NSFS_MAGIC.
#define NSFS_MAGIC 0x6e736673
#endif
	if (buf.f_type == NSFS_MAGIC || buf.f_type == PROC_SUPER_MAGIC) {
		char *vanilla_cwd __attribute__ ((cleanup(sc_cleanup_string))) =
		    NULL;
		vanilla_cwd = get_current_dir_name();
		if (vanilla_cwd == NULL) {
			die("cannot get the current working directory");
		}
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
		return;
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
		char src[PATH_MAX];
		char dst[PATH_MAX];
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
}

bool sc_should_populate_ns_group(struct sc_ns_group *group)
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
