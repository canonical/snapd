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

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdio.h>
#include <string.h>
#include <stdint.h>
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

// Keep in sync with snap-seccomp/main.go
//
// Header of a seccomp.bin2 filter file in native byte order.
struct __attribute__((__packed__)) sc_seccomp_file_header {
	// header: "SC"
	char header[2];
	// version: 0x1
	uint8_t version;
	// flags
	uint8_t unrestricted;
	// unused
	uint8_t padding[4];

	// size of allow filter in byte
	uint32_t len_allow_filter;
	// size of deny filter in byte
	uint32_t len_deny_filter;
	// reserved for future use
	uint8_t reserved2[112];
};

static_assert(sizeof(struct sc_seccomp_file_header) == 128,
	      "unexpected struct size");

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

static void sc_cleanup_sock_fprog(struct sock_fprog *prog)
{
	free(prog->filter);
	prog->filter = NULL;
}

static void sc_must_read_filter_from_file(FILE *file, uint32_t len_bytes,
					  char *what, struct sock_fprog *prog)
{
	if (len_bytes == 0) {
		die("%s filter may only be empty in unrestricted profiles",
		    what);
	}
	prog->len = len_bytes / sizeof(struct sock_filter);
	prog->filter = (struct sock_filter *)malloc(len_bytes);
	if (prog->filter == NULL) {
		die("cannot allocate %u bytes of memory for %s seccomp filter ",
		    len_bytes, what);
	}
	size_t num_read = fread(prog->filter, 1, len_bytes, file);
	if (ferror(file)) {
		die("cannot read %s filter", what);
	}
	if (num_read != len_bytes) {
		die("short read for filter %s %zu != %i", what, num_read,
		    len_bytes);
	}
}

static FILE *sc_must_read_and_validate_header_from_file(const char
							*profile_path, struct
							sc_seccomp_file_header
							*hdr)
{
	FILE *file = fopen(profile_path, "rb");
	if (file == NULL) {
		die("cannot open seccomp filter %s", profile_path);
	}
	size_t num_read =
	    fread(hdr, 1, sizeof(struct sc_seccomp_file_header), file);
	if (ferror(file) != 0) {
		die("cannot read seccomp profile %s", profile_path);
	}
	if (num_read < sizeof(struct sc_seccomp_file_header)) {
		die("short read on seccomp header: %zu", num_read);
	}
	if (hdr->header[0] != 'S' || hdr->header[1] != 'C') {
		die("unexpected seccomp header: %x%x", hdr->header[0],
		    hdr->header[1]);
	}
	if (hdr->version != 1) {
		die("unexpected seccomp file version: %x", hdr->version);
	}
	if (hdr->len_allow_filter > MAX_BPF_SIZE) {
		die("allow filter size too big %u", hdr->len_allow_filter);
	}
	if (hdr->len_allow_filter % sizeof(struct sock_filter) != 0) {
		die("allow filter size not multiple of sock_filter");
	}
	if (hdr->len_deny_filter > MAX_BPF_SIZE) {
		die("deny filter size too big %u", hdr->len_deny_filter);
	}
	if (hdr->len_deny_filter % sizeof(struct sock_filter) != 0) {
		die("deny filter size not multiple of sock_filter");
	}
	struct stat stat_buf;
	if (fstat(fileno(file), &stat_buf) != 0) {
		die("cannot fstat the seccomp file");
	}
	off_t expected_size =
	    sizeof(struct sc_seccomp_file_header) + hdr->len_allow_filter +
	    hdr->len_deny_filter;
	if (stat_buf.st_size != expected_size) {
		die("unexpected filesize %ju != %ju", stat_buf.st_size,
		    expected_size);
	}

	return file;
}

bool sc_apply_seccomp_profile_for_security_tag(const char *security_tag)
{
	debug("loading bpf program for security tag %s", security_tag);

	char profile_path[PATH_MAX] = { 0 };
	struct sock_fprog SC_CLEANUP(sc_cleanup_sock_fprog) prog_allow = { 0 };
	struct sock_fprog SC_CLEANUP(sc_cleanup_sock_fprog) prog_deny = { 0 };
	sc_must_snprintf(profile_path, sizeof(profile_path), "%s/%s.bin2",
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
	for (long i = 0; i < max_wait; ++i) {
		if (access(profile_path, F_OK) == 0) {
			break;
		}
		sleep(1);
	}

	// TODO: move over to open/openat as an additional hardening measure.

	// validate '/' down to profile_path are root-owned and not
	// 'other' writable to avoid possibility of privilege
	// escalation via bpf program load when paths are incorrectly
	// set on the system.
	validate_bpfpath_is_safe(profile_path);

	// workaround bug in gcc from 14.04, the pragma can be removed when
	// we stop supporting 14.04
#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wmissing-braces"
	struct sc_seccomp_file_header hdr = { 0 };
#pragma GCC diagnostic pop
	FILE *file SC_CLEANUP(sc_cleanup_file) =
	    sc_must_read_and_validate_header_from_file(profile_path, &hdr);
	if (hdr.unrestricted == 0x1) {
		return false;
	}
	// populate allow
	sc_must_read_filter_from_file(file, hdr.len_allow_filter, "allow",
				      &prog_allow);
	sc_must_read_filter_from_file(file, hdr.len_deny_filter, "deny",
				      &prog_deny);

	// apply both filters
	sc_apply_seccomp_filter(&prog_deny);
	sc_apply_seccomp_filter(&prog_allow);

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
