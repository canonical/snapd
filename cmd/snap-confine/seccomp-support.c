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

#include <errno.h>
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

static char *filter_profile_dir = "/var/lib/snapd/seccomp/bpf/";

// MAX_BPF_SIZE is an arbitrary limit.
const int MAX_BPF_SIZE = 640 * 1024;

typedef struct sock_filter bpf_instr;

static bool is_valid_bpf_opcode(__u16 code)
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

static void validate_bpf(void *buf, size_t buf_size)
{
	bpf_instr *bpf = buf;

	while ((void *)bpf < buf + buf_size) {
		if (!is_valid_bpf_opcode(bpf->code)) {
			errno = 0;
			die("opcode %x is unknown", bpf->code);
		}
		bpf++;
	}
}

static void validate_path_has_strict_perms(const char *path)
{
	struct stat stat_buf;
	if (stat(path, &stat_buf) < 0)
		die("cannot stat %s", path);

	errno = 0;
	if (stat_buf.st_uid != 0 || stat_buf.st_gid != 0)
		die("%s not root-owned %i:%i", path, stat_buf.st_uid,
		    stat_buf.st_gid);

	if (stat_buf.st_mode & S_IWOTH)
		die("%s has 'other' write %o", path, stat_buf.st_mode);
}

static void validate_bpfpath_is_safe(const char *path)
{
	if (path == NULL || strlen(path) == 0 || path[0] != '/')
		die("valid_bpfpath_is_safe needs an absolute path as input");

	// strtok_r() modifies its first argument, so work on a copy
	char *tokenized = strdup(path);

	// allocate a string large enough to hold path, and initialize it to
	// '/'
	size_t checked_path_size = sizeof(char) * strlen(path) + 1;
	char *checked_path = malloc(checked_path_size);
	if (checked_path == NULL)
		die("Out of memory creating checked_path");

	checked_path[0] = '/';
	checked_path[1] = '\0';

	// validate '/'
	validate_path_has_strict_perms(checked_path);

	// strtok_r needs a pointer to keep track of where it is in the
	// string.
	char *buf_saveptr = NULL;

	// reconstruct the path from '/' down to profile_name
	char *buf_token = strtok_r(tokenized, "/", &buf_saveptr);
	while (buf_token != NULL) {
		char *prev = strdup(checked_path);	// needed by vsnprintf in sc_must_snprintf
		// append '<buf_token>' if checked_path is '/', otherwise '/<buf_token>'
		if (strlen(checked_path) == 1)
			sc_must_snprintf(checked_path, checked_path_size,
					 "%s%s", prev, buf_token);
		else
			sc_must_snprintf(checked_path, checked_path_size,
					 "%s/%s", prev, buf_token);
		free(prev);
		validate_path_has_strict_perms(checked_path);

		buf_token = strtok_r(NULL, "/", &buf_saveptr);
	}

	free(tokenized);
	free(checked_path);
}

int sc_apply_seccomp_bpf(const char *filter_profile)
{
	debug("loading bpf program for security tag %s", filter_profile);

	char profile_path[512];	// arbitrary path name limit
	sc_must_snprintf(profile_path, sizeof(profile_path), "%s/%s.bpf",
			 filter_profile_dir, filter_profile);

	// validate '/' down to profile_path are root-owned and not
	// 'other' writable to avoid possibility of privilege
	// escalation via bpf program load when paths are incorrectly
	// set on the system.
	validate_bpfpath_is_safe(profile_path);

	// sanity check size/owner
	struct stat stat_buf;
	if (stat(profile_path, &stat_buf) < 0)
		die("cannot stat %s", profile_path);
	if (stat_buf.st_size > MAX_BPF_SIZE)
		die("profile %s is too big", profile_path);

	// paranoid, but only helps a bit
	if (stat_buf.st_uid != 0 || stat_buf.st_gid != 0)
		die("cannot use %s: must be root:root owned", profile_path);

	// load bpf
	unsigned char bpf[MAX_BPF_SIZE + 1];	// acount for EOF
	FILE *fp = fopen(profile_path, "rb");
	if (fp == NULL)
		die("cannot read %s", profile_path);
	// set 'size' to 1 to get bytes transferred
	size_t num_read = fread(bpf, 1, sizeof(bpf), fp);
	if (ferror(fp) != 0)
		die("cannot fread() %s", profile_path);
	else if (feof(fp) == 0)
		die("profile %s exceeds %zu bytes", profile_path, sizeof(bpf));
	fclose(fp);
	debug("read %zu bytes from %s", num_read, profile_path);

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
