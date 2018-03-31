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
#include <limits.h>
#include <stdio.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/types.h>
#include <unistd.h>

#include <linux/filter.h>
#include <linux/seccomp.h>

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/secure-getenv.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

#ifndef SECCOMP_FILTER_FLAG_LOG
#define SECCOMP_FILTER_FLAG_LOG 2
#endif

#ifndef seccomp
// prototype because we build with -Wstrict-prototypes
int seccomp(unsigned int operation, unsigned int flags, void *args);

int seccomp(unsigned int operation, unsigned int flags, void *args)
{
	errno = 0;
	return syscall(__NR_seccomp, operation, flags, args);
}
#endif

static const char *filter_profile_dir = "/var/lib/snapd/seccomp/bpf/";

// MAX_BPF_SIZE is an arbitrary limit.
#define MAX_BPF_SIZE (32 * 1024)

typedef struct sock_filter bpf_instr;

static void validate_path_has_strict_perms(const char *path)
{
	struct stat stat_buf;
	if (stat(path, &stat_buf) < 0) {
		die("cannot stat %s", path);
	}

	errno = 0;
	if (stat_buf.st_uid != 0 || stat_buf.st_gid != 0) {
		die("%s not root-owned %i:%i", path, stat_buf.st_uid,
		    stat_buf.st_gid);
	}

	if (stat_buf.st_mode & S_IWOTH) {
		die("%s has 'other' write %o", path, stat_buf.st_mode);
	}
}

static void validate_bpfpath_is_safe(const char *path)
{
	if (path == NULL || strlen(path) == 0 || path[0] != '/') {
		die("valid_bpfpath_is_safe needs an absolute path as input");
	}
	// strtok_r() modifies its first argument, so work on a copy
	char *tokenized SC_CLEANUP(sc_cleanup_string) = NULL;
	tokenized = strdup(path);
	if (tokenized == NULL) {
		die("cannot allocate memory for copy of path");
	}
	// allocate a string large enough to hold path, and initialize it to
	// '/'
	size_t checked_path_size = strlen(path) + 1;
	char *checked_path SC_CLEANUP(sc_cleanup_string) = NULL;
	checked_path = calloc(checked_path_size, 1);
	if (checked_path == NULL) {
		die("cannot allocate memory for checked_path");
	}

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
		char *prev SC_CLEANUP(sc_cleanup_string) = NULL;
		prev = strdup(checked_path);	// needed by vsnprintf in sc_must_snprintf
		if (prev == NULL) {
			die("cannot allocate memory for copy of checked_path");
		}
		// append '<buf_token>' if checked_path is '/', otherwise '/<buf_token>'
		if (strlen(checked_path) == 1) {
			sc_must_snprintf(checked_path, checked_path_size,
					 "%s%s", prev, buf_token);
		} else {
			sc_must_snprintf(checked_path, checked_path_size,
					 "%s/%s", prev, buf_token);
		}
		validate_path_has_strict_perms(checked_path);

		buf_token = strtok_r(NULL, "/", &buf_saveptr);
	}
}

int sc_apply_seccomp_bpf(const char *filter_profile)
{
	debug("loading bpf program for security tag %s", filter_profile);

	char profile_path[PATH_MAX] = { 0 };
	sc_must_snprintf(profile_path, sizeof(profile_path), "%s/%s.bin",
			 filter_profile_dir, filter_profile);

	// Wait some time for the security profile to show up. When
	// the system boots snapd will created security profiles, but
	// a service snap (e.g. network-manager) starts in parallel with
	// snapd so for such snaps, the profiles may not be generated
	// yet
	long max_wait = 120;
	const char *MAX_PROFILE_WAIT = getenv("SNAP_CONFINE_MAX_PROFILE_WAIT");
	if (MAX_PROFILE_WAIT != NULL) {
		char *endptr = NULL;
		errno = 0;
		long env_max_wait = strtol(MAX_PROFILE_WAIT, &endptr, 10);
		if (errno != 0 || MAX_PROFILE_WAIT == endptr || *endptr != '\0'
		    || env_max_wait <= 0) {
			die("SNAP_CONFINE_MAX_PROFILE_WAIT invalid");
		}
		max_wait = env_max_wait > 0 ? env_max_wait : max_wait;
	}
	if (max_wait > 3600) {
		max_wait = 3600;
	}
	for (long i = 0; i < max_wait; ++i) {
		if (access(profile_path, F_OK) == 0) {
			break;
		}
		sleep(1);
	}

	// validate '/' down to profile_path are root-owned and not
	// 'other' writable to avoid possibility of privilege
	// escalation via bpf program load when paths are incorrectly
	// set on the system.
	validate_bpfpath_is_safe(profile_path);

	// load bpf
	char bpf[MAX_BPF_SIZE + 1] = { 0 };	// account for EOF
	FILE *fp = fopen(profile_path, "rb");
	if (fp == NULL) {
		die("cannot read %s", profile_path);
	}
	// set 'size' to 1 to get bytes transferred
	size_t num_read = fread(bpf, 1, sizeof(bpf), fp);
	if (ferror(fp) != 0) {
		die("cannot read seccomp profile %s", profile_path);
	} else if (feof(fp) == 0) {
		die("seccomp profile %s exceeds %zu bytes", profile_path,
		    sizeof(bpf));
	}
	fclose(fp);
	debug("read %zu bytes from %s", num_read, profile_path);

	if (sc_streq(bpf, "@unrestricted\n")) {
		return 0;
	}

	uid_t real_uid, effective_uid, saved_uid;
	if (getresuid(&real_uid, &effective_uid, &saved_uid) < 0) {
		die("cannot call getresuid");
	}
	// If we can, raise privileges so that we can load the BPF into the
	// kernel via 'prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, ...)'.
	debug("raising privileges to load seccomp profile");
	if (effective_uid != 0 && saved_uid == 0) {
		if (seteuid(0) != 0) {
			die("seteuid failed");
		}
		if (geteuid() != 0) {
			die("raising privs before seccomp_load did not work");
		}
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
	if (seccomp(SECCOMP_SET_MODE_FILTER, SECCOMP_FILTER_FLAG_LOG, &prog) !=
	    0) {
		if (errno == ENOSYS) {
			debug("kernel doesn't support the seccomp(2) syscall");
		} else if (errno == EINVAL) {
			debug
			    ("kernel may not support the SECCOMP_FILTER_FLAG_LOG flag");
		}

		debug
		    ("falling back to prctl(2) syscall to load seccomp filter");
		if (prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, &prog) != 0) {
			die("cannot apply seccomp profile");
		}
	}
	// drop privileges again
	debug("dropping privileges after loading seccomp profile");
	if (geteuid() == 0) {
		unsigned real_uid = getuid();
		if (seteuid(real_uid) != 0) {
			die("seteuid failed");
		}
		if (real_uid != 0 && geteuid() == 0) {
			die("dropping privs after seccomp_load did not work");
		}
	}

	return 0;
}
