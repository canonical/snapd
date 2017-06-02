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

#include <asm/ioctls.h>
#include <ctype.h>
#include <errno.h>
#include <fcntl.h>
#include <linux/can.h>		// needed for search mappings
#include <linux/netlink.h>
#include <sched.h>
#include <search.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/quota.h>
#include <sys/resource.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/utsname.h>
#include <termios.h>
#include <unistd.h>

// The XFS interface requires a 64 bit file system interface
// but we don't want to leak this anywhere else if not globally
// defined.
#ifndef _FILE_OFFSET_BITS
#define _FILE_OFFSET_BITS 64
#include <xfs/xqm.h>
#undef _FILE_OFFSET_BITS
#else
#include <xfs/xqm.h>
#endif

#include <seccomp.h>
#include <linux/filter.h>
#include <linux/seccomp.h>

#include "../libsnap-confine-private/secure-getenv.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"


static char *filter_profile_dir = "/var/lib/snapd/seccomp/profiles/";


int sc_apply_seccomp_bpf(const char *filter_profile)
{
	debug("loading bpf program for security tag %s", filter_profile);

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

	char profile_path[512];	// arbitrary path name limit
	sc_must_snprintf(profile_path, sizeof(profile_path), "%s/%s.bpf",
			 filter_profile_dir, filter_profile);

	// load bpf
	char bpf[32 * 1024];
	int fd = open(profile_path, O_RDONLY);
	if (fd < 0)
		die("cannot read %s", profile_path);

	ssize_t num_read = read(fd, bpf, sizeof bpf);
	if (num_read < 0) {
		die("cannot read bpf %s", profile_path);
	} else if (num_read == sizeof bpf) {
		die("cannot fit bpf %s into buffer", profile_path);
	}
	close(fd);

	// Disable NO_NEW_PRIVS because it interferes with exec transitions in
	// AppArmor. Unfortunately this means that security policies must be
	// very careful to not allow the following otherwise apps can escape
	// the sandbox:
	//   - seccomp syscall
	//   - prctl with PR_SET_SECCOMP
	//   - ptrace (trace) in AppArmor
	//   - capability sys_admin in AppArmor
	// Note that with NO_NEW_PRIVS disabled, CAP_SYS_ADMIN is required to
	// change the seccomp sandbox.
	struct sock_fprog prog = {
		.len = num_read / sizeof(struct sock_filter),
		.filter = (struct sock_filter *)bpf,
	};
	if (prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, &prog)) {
		die("prctl(SECCOMP)");
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

