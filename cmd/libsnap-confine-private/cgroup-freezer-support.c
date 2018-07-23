// For AT_EMPTY_PATH and O_PATH
#define _GNU_SOURCE

#include "cgroup-freezer-support.h"

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#include "cleanup-funcs.h"
#include "string-utils.h"
#include "utils.h"

static const char *freezer_cgroup_dir = "/sys/fs/cgroup/freezer";

void sc_cgroup_freezer_join(const char *snap_name, pid_t pid)
{
	// Format the name of the cgroup hierarchy.
	char buf[PATH_MAX] = { 0 };
	sc_must_snprintf(buf, sizeof buf, "snap.%s", snap_name);

	// Open the freezer cgroup directory.
	int cgroup_fd SC_CLEANUP(sc_cleanup_close) = -1;
	cgroup_fd = open(freezer_cgroup_dir,
			 O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
	if (cgroup_fd < 0) {
		die("cannot open freezer cgroup (%s)", freezer_cgroup_dir);
	}
	// Create the freezer hierarchy for the given snap.
	if (mkdirat(cgroup_fd, buf, 0755) < 0 && errno != EEXIST) {
		die("cannot create freezer cgroup hierarchy for snap %s",
		    snap_name);
	}
	// Open the hierarchy directory for the given snap.
	int hierarchy_fd SC_CLEANUP(sc_cleanup_close) = -1;
	hierarchy_fd = openat(cgroup_fd, buf,
			      O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
	if (hierarchy_fd < 0) {
		die("cannot open freezer cgroup hierarchy for snap %s",
		    snap_name);
	}
	// Since we may be running from a setuid but not setgid executable, ensure
	// that the group and owner of the hierarchy directory is root.root.
	if (fchownat(hierarchy_fd, "", 0, 0, AT_EMPTY_PATH) < 0) {
		die("cannot change owner of freezer cgroup hierarchy for snap %s to root.root", snap_name);
	}
	// Open the tasks file.
	int tasks_fd SC_CLEANUP(sc_cleanup_close) = -1;
	tasks_fd = openat(hierarchy_fd, "tasks",
			  O_WRONLY | O_NOFOLLOW | O_CLOEXEC);
	if (tasks_fd < 0) {
		die("cannot open tasks file for freezer cgroup hierarchy for snap %s", snap_name);
	}
	// Write the process (task) number to the tasks file. Linux task IDs are
	// limited to 2^29 so a long int is enough to represent it.
	// See include/linux/threads.h in the kernel source tree for details.
	int n = sc_must_snprintf(buf, sizeof buf, "%ld", (long)pid);
	if (write(tasks_fd, buf, n) < n) {
		die("cannot move process %ld to freezer cgroup hierarchy for snap %s", (long)pid, snap_name);
	}
	debug("moved process %ld to freezer cgroup hierarchy for snap %s",
	      (long)pid, snap_name);
}

bool sc_cgroup_freezer_occupied(const char *snap_name)
{
	// Format the name of the cgroup hierarchy.
	char buf[PATH_MAX] = { 0 };
	sc_must_snprintf(buf, sizeof buf, "snap.%s", snap_name);

	// Open the freezer cgroup directory.
	int cgroup_fd SC_CLEANUP(sc_cleanup_close) = -1;
	cgroup_fd = open(freezer_cgroup_dir,
			 O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
	if (cgroup_fd < 0) {
		die("cannot open freezer cgroup (%s)", freezer_cgroup_dir);
	}
	// Open the proc directory.
	int proc_fd SC_CLEANUP(sc_cleanup_close) = -1;
	proc_fd = open("/proc", O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
	if (proc_fd < 0) {
		die("cannot open /proc");
	}
	// Open the hierarchy directory for the given snap.
	int hierarchy_fd SC_CLEANUP(sc_cleanup_close) = -1;
	hierarchy_fd = openat(cgroup_fd, buf,
			      O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
	if (hierarchy_fd < 0) {
		if (errno == ENOENT) {
			return false;
		}
		die("cannot open freezer cgroup hierarchy for snap %s",
		    snap_name);
	}
	// Open the "cgroup.procs" file. Alternatively we could open the "tasks"
	// file and see per-thread data but we don't need that.
	int cgroup_procs_fd SC_CLEANUP(sc_cleanup_close) = -1;
	cgroup_procs_fd = openat(hierarchy_fd, "cgroup.procs",
				 O_RDONLY | O_NOFOLLOW | O_CLOEXEC);
	if (cgroup_procs_fd < 0) {
		die("cannot open cgroup.procs file for freezer cgroup hierarchy for snap %s", snap_name);
	}

	FILE *cgroup_procs SC_CLEANUP(sc_cleanup_file) = NULL;
	cgroup_procs = fdopen(cgroup_procs_fd, "r");
	if (cgroup_procs == NULL) {
		die("cannot convert tasks file descriptor to FILE");
	}
	cgroup_procs_fd = -1;	// cgroup_procs_fd will now be closed by fclose.

	char *line_buf SC_CLEANUP(sc_cleanup_string) = NULL;
	size_t line_buf_size = 0;
	ssize_t num_read;
	struct stat statbuf;
	do {
		num_read = getline(&line_buf, &line_buf_size, cgroup_procs);
		if (num_read < 0 && errno != 0) {
			die("cannot read next PID belonging to snap %s",
			    snap_name);
		}
		if (num_read <= 0) {
			break;
		} else {
			if (line_buf[num_read - 1] == '\n') {
				line_buf[num_read - 1] = '\0';
			} else {
				die("could not find newline in cgroup.procs");
			}
		}
		debug("found process id: %s\n", line_buf);

		if (fstatat(proc_fd, line_buf, &statbuf, AT_SYMLINK_NOFOLLOW) <
		    0) {
			// The process may have died already.
			if (errno != ENOENT) {
				die("cannot stat /proc/%s", line_buf);
			}
		}
		debug("found process %s belonging to user %d",
		      line_buf, statbuf.st_uid);
		return true;
	} while (num_read > 0);

	return false;
}
