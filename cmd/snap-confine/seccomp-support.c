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

#include "seccomp-support-ext.h"

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
	tokenized = sc_strdup(path);
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
		prev = sc_strdup(checked_path);	// needed by vsnprintf in sc_must_snprintf
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

bool sc_apply_seccomp_profile_for_security_tag(const char *security_tag)
{
	debug("loading bpf program for security tag %s", security_tag);

	char profile_path[PATH_MAX] = { 0 };
	sc_must_snprintf(profile_path, sizeof(profile_path), "%s/%s.bin",
			 filter_profile_dir, security_tag);

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

	if (!sc_wait_for_file(profile_path, max_wait)) {
		/* log but proceed, we'll die a bit later */
		debug("timeout waiting for seccomp binary profile file at %s",
		      profile_path);
	}
	// TODO: move over to open/openat as an additional hardening measure.

	// validate '/' down to profile_path are root-owned and not
	// 'other' writable to avoid possibility of privilege
	// escalation via bpf program load when paths are incorrectly
	// set on the system.
	validate_bpfpath_is_safe(profile_path);

	/* The extra space has dual purpose. First of all, it is required to detect
	 * feof() while still being able to correctly read MAX_BPF_SIZE bytes of
	 * seccomp profile.  In addition, because we treat the profile as a
	 * quasi-string and use sc_streq(), to compare it. The extra space is used
	 * as a way to ensure the result is a terminated string (though in practice
	 * it can contain embedded NULs any earlier position). Note that
	 * sc_read_seccomp_filter knows about the extra space and ensures that the
	 * buffer is never empty. */
	char bpf[MAX_BPF_SIZE + 1] = { 0 };
	size_t num_read = sc_read_seccomp_filter(profile_path, bpf, sizeof bpf);
	if (sc_streq(bpf, "@unrestricted\n")) {
		return false;
	}
	struct sock_fprog prog = {
		.len = num_read / sizeof(struct sock_filter),
		.filter = (struct sock_filter *)bpf,
	};
	sc_apply_seccomp_filter(&prog);
	return true;
}

void sc_apply_global_seccomp_profile(void)
{
	const char *profile_path = "/var/lib/snapd/seccomp/bpf/global.bin";
	/* The profile may be absent. */
	if (access(profile_path, F_OK) != 0) {
		return;
	}
	// TODO: move over to open/openat as an additional hardening measure.
	validate_bpfpath_is_safe(profile_path);

	char bpf[MAX_BPF_SIZE + 1] = { 0 };
	size_t num_read = sc_read_seccomp_filter(profile_path, bpf, sizeof bpf);
	struct sock_fprog prog = {
		.len = num_read / sizeof(struct sock_filter),
		.filter = (struct sock_filter *)bpf,
	};
	sc_apply_seccomp_filter(&prog);
}
