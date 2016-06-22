/*
 * Copyright (C) 2015 Canonical Ltd
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
#include <stdio.h>
#include <unistd.h>
#include <string.h>
#include <ctype.h>
#include <stdbool.h>
#include <stdlib.h>
#include <sys/prctl.h>

#include <seccomp.h>

#include "utils.h"

#define SC_MAX_LINE_LENGTH	82	// 80 + '\n' + '\0'

char *filter_profile_dir = "/var/lib/snapd/seccomp/profiles/";

struct preprocess {
	bool unrestricted;
	bool complain;
};

// strip whitespace from the end of the given string (inplace)
static size_t trim_right(char *s, size_t slen)
{
	while (slen > 0 && isspace(s[slen - 1])) {
		s[--slen] = 0;
	}
	return slen;
}

// Read a relevant line and return the length. Return length '0' for comments,
// empty lines and lines with only whitespace (so a caller can easily skip
// them). The line buffer is right whitespaced trimmed and the final length of
// the trimmed line is returned.
static size_t validate_and_trim_line(char *buf, size_t buf_len, size_t lineno)
{
	size_t len = 0;

	// comment, ignore
	if (buf[0] == '#')
		return len;

	// ensure the entire line was read
	len = strlen(buf);
	if (len == 0)
		return len;
	else if (buf[len - 1] != '\n' && len > (buf_len - 2)) {
		fprintf(stderr,
			"seccomp filter line %zu was too long (%zu characters max)\n",
			lineno, buf_len - 2);
		errno = 0;
		die("aborting");
	}
	// kill final newline
	len = trim_right(buf, len);

	return len;
}

static void preprocess_filter(FILE * f, struct preprocess *p)
{
	char buf[SC_MAX_LINE_LENGTH];
	size_t lineno = 0;

	p->unrestricted = false;
	p->complain = false;

	while (fgets(buf, sizeof(buf), f) != NULL) {
		lineno++;

		// skip policy-irrelevant lines
		if (validate_and_trim_line(buf, sizeof(buf), lineno) == 0)
			continue;

		// check for special "@unrestricted" rule which short-circuits
		// seccomp sandbox
		if (strcmp(buf, "@unrestricted") == 0)
			p->unrestricted = true;

		// check for special "@complain" rule
		if (strcmp(buf, "@complain") == 0)
			p->complain = true;
	}

	if (fseek(f, 0L, SEEK_SET) != 0)
		die("could not rewind file");

	return;
}

void seccomp_load_filters(const char *filter_profile)
{
	debug("seccomp_load_filters %s", filter_profile);
	int rc = 0;
	int syscall_nr = -1;
	scmp_filter_ctx ctx = NULL;
	FILE *f = NULL;
	size_t lineno = 0;
	uid_t real_uid, effective_uid, saved_uid;
	struct preprocess pre;

	ctx = seccomp_init(SCMP_ACT_KILL);
	if (ctx == NULL) {
		errno = ENOMEM;
		die("seccomp_init() failed");
	}
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

	if (getresuid(&real_uid, &effective_uid, &saved_uid) != 0)
		die("could not find user IDs");

	// If running privileged or capable of raising, disable nnp
	if (real_uid == 0 || effective_uid == 0 || saved_uid == 0)
		if (seccomp_attr_set(ctx, SCMP_FLTATR_CTL_NNP, 0) != 0)
			die("Cannot disable nnp");

	// Note that secure_gettenv will always return NULL when suid, so
	// SNAPPY_LAUNCHER_SECCOMP_PROFILE_DIR can't be (ab)used in that case.
	if (secure_getenv("SNAPPY_LAUNCHER_SECCOMP_PROFILE_DIR") != NULL)
		filter_profile_dir =
		    secure_getenv("SNAPPY_LAUNCHER_SECCOMP_PROFILE_DIR");

	char profile_path[512];	// arbitrary path name limit
	must_snprintf(profile_path, sizeof(profile_path), "%s/%s",
		      filter_profile_dir, filter_profile);

	f = fopen(profile_path, "r");
	if (f == NULL) {
		fprintf(stderr, "Can not open %s (%s)\n", profile_path,
			strerror(errno));
		die("aborting");
	}
	// Note, preprocess_filter() die()s on error
	preprocess_filter(f, &pre);

	if (pre.unrestricted)
		goto out;

	// FIXME: right now complain mode is the equivalent to unrestricted.
	// We'll want to change this once we seccomp logging is in order.
	if (pre.complain)
		goto out;

	char buf[SC_MAX_LINE_LENGTH];
	while (fgets(buf, sizeof(buf), f) != NULL) {
		lineno++;

		// skip policy-irrelevant lines
		if (validate_and_trim_line(buf, sizeof(buf), lineno) == 0)
			continue;

		// syscall not available on this arch/kernel
		// as this is a syscall whitelist its ok and the error can be
		// ignored
		syscall_nr = seccomp_syscall_resolve_name(buf);
		if (syscall_nr == __NR_SCMP_ERROR)
			continue;

		// a normal line with a syscall
		rc = seccomp_rule_add_exact(ctx, SCMP_ACT_ALLOW, syscall_nr, 0);
		if (rc != 0) {
			rc = seccomp_rule_add(ctx, SCMP_ACT_ALLOW, syscall_nr,
					      0);
			if (rc != 0) {
				fprintf(stderr,
					"seccomp_rule_add failed with %i for '%s'\n",
					rc, buf);
				errno = 0;
				die("aborting");
			}
		}
	}

	// If not root but can raise, then raise privileges to load seccomp
	// policy since we don't have nnp
	if (effective_uid != 0 && saved_uid == 0) {
		if (seteuid(0) != 0)
			die("seteuid failed");
		if (geteuid() != 0)
			die("raising privs before seccomp_load did not work");
	}
	// load it into the kernel
	rc = seccomp_load(ctx);

	if (rc != 0) {
		fprintf(stderr, "seccomp_load failed with %i\n", rc);
		die("aborting");
	}
	// drop privileges again
	if (geteuid() == 0) {
		unsigned real_uid = getuid();
		if (seteuid(real_uid) != 0)
			die("seteuid failed");
		if (real_uid != 0 && geteuid() == 0)
			die("dropping privs after seccomp_load did not work");
	}

 out:
	if (f != NULL) {
		if (fclose(f) != 0)
			die("could not close seccomp file");
	}
	seccomp_release(ctx);
	return;
}
