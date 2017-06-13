/*
 * Copyright (C) 2015-2017 Canonical Ltd
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
#include "seccomp-support.h"

#include <fcntl.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>

#include <linux/filter.h>
#include <linux/seccomp.h>

#include "../libsnap-confine-private/secure-getenv.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

static char *filter_profile_dir = "/var/lib/snapd/seccomp/profiles/";

// MAX_BPF_SIZE is an arbitrary limit.
const int MAX_BPF_SIZE = 640 * 1024;

int sc_apply_seccomp_bpf(const char *filter_profile)
{
	debug("loading bpf program for security tag %s", filter_profile);

	char profile_path[512];	// arbitrary path name limit
	sc_must_snprintf(profile_path, sizeof(profile_path), "%s/%s.bpf",
			 filter_profile_dir, filter_profile);

	// load bpf
	char bpf[MAX_BPF_SIZE];
	int fd = open(profile_path, O_RDONLY);
	if (fd < 0)
		die("cannot read %s", profile_path);
	struct stat stat_buf;

	if (fstat(fd, &stat_buf) < 0)
		die("cannot stat %s", profile_path);
	if (stat_buf.st_size > MAX_BPF_SIZE)
		die("profile %s is too big %lu", profile_path,
		    stat_buf.st_size);

	// FIXME: make this a robust read that deals with e.g. deal with
	//        e.g. interrupts by signals
	ssize_t num_read = read(fd, bpf, sizeof bpf);
	if (num_read < 0) {
		die("cannot read bpf %s", profile_path);
	}
	if (num_read < stat_buf.st_size) {
		die("cannot read bpf file %s, only got %lu instead of %lu",
		    profile_path, num_read, stat_buf.st_size);
	}
	close(fd);

	// raise privs
	uid_t real_uid, effective_uid, saved_uid;
	if (getresuid(&real_uid, &effective_uid, &saved_uid) != 0)
		die("could not find user IDs");
	// If not root but can raise, then raise privileges to load seccomp
	// policy since we don't have nnp
	debug("raising privileges to load seccomp profile");
	if (effective_uid != 0 && saved_uid == 0) {
		if (seteuid(0) != 0)
			die("seteuid failed");
		if (geteuid() != 0)
			die("raising privs before seccomp_load did not work");
	}
	// Load filter into the kernel. Importantly we are
	// intentionally *not* setting NO_NEW_PRIVS because it
	// interferes with exec transitions in AppArmor with certain
	// snappy interfaces. Not setting NO_NEW_PRIVS does mean that
	// applications can adjust their sandbox if they have
	// CAP_SYS_ADMIN or, if running on < 4.8 kernels, break out of
	// the seccomp via ptrace. Both CAP_SYS_ADMIN and 'ptrace
	// (trace)' are blocked by AppArmor with typical snappy
	// interfaces.
	struct sock_fprog prog = {
		.len = num_read / sizeof(struct sock_filter),
		.filter = (struct sock_filter *)bpf,
	};
	if (prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, &prog)) {
		perror
		    ("prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, ...) failed");
		die("aborting");
	}
	// drop privileges again
	debug("dropping privileges after loading seccomp profile");
	if (geteuid() == 0) {
		unsigned real_uid = getuid();
		if (seteuid(real_uid) != 0)
			die("seteuid failed");
		if (real_uid != 0 && geteuid() == 0)
			die("dropping privs after seccomp_load did not work");
	}

	return 0;
}
