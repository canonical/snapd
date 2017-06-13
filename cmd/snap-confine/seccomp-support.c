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
#include <stdio.h>
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

typedef struct sock_filter bpf_instr;

bool is_valid_bpf_opcode(__u16 code)
{
	// from https://github.com/iovisor/bpf-docs/blob/master/eBPF.md 
	__u16 valid_opcodes[] = {
		// 64-bit
		0x07, 0x0f, 0x17, 0x1f, 0x27, 0x2f, 0x37, 0x3f, 0x47, 0x4f,
		0x57, 0x5f, 0x67, 0x6f, 0x77, 0x7f, 0x87, 0x97, 0x9f, 0xa7,
		0xaf, 0xb7, 0xbf, 0xc7, 0xcf,
		// 32-bit
		0x04, 0x0c, 0x14, 0x1c, 0x24, 0x2c, 0x34, 0x3c, 0x44, 0x4c,
		0x54, 0x5c, 0x64, 0x6c, 0x74, 0x7c, 0x84, 0x94, 0x9c, 0xa4,
		0xac, 0xb4, 0xbc, 0xc4, 0xcc,
		// byteswap
		0xd4, 0xdc,
		// memory-instructions
		0x18, 0x20, 0x28, 0x30, 0x38, 0x40, 0x48, 0x50, 0x58, 0x61,
		0x69, 0x71, 0x79, 0x62, 0x6a, 0x72, 0x7a, 0x63, 0x6b, 0x73,
		0x7b,
		// branch
		0x05, 0x15, 0x1d, 0x25, 0x2d, 0x35, 0x3d, 0x45, 0x4d, 0x55,
		0x5d, 0x65, 0x6d, 0x75, 0x7d, 0x85, 0x95,
		// return (not mentioned in the above url)
		0x6,
	};

	for (int i = 0; i < sizeof(valid_opcodes) / sizeof(__u16); i++) {
		if (code == valid_opcodes[i]) {
			return true;
		}
	}
	return false;
}

void validate_bpf(void *buf, size_t buf_size)
{
	bpf_instr *bpf = buf;

	while ((void *)bpf < buf + buf_size) {
		if (!is_valid_bpf_opcode(bpf->code))
			die("opcode %x is unknown", bpf->code);

		bpf++;
	}
}

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

	// validate bpf, it will die() if things look wrong
	validate_bpf(bpf, num_read);

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
