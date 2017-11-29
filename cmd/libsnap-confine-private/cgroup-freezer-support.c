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
