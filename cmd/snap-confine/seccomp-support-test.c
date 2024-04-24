/*
 * Copyright (C) 2024 Canonical Ltd
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

#include "seccomp-support-ext.c"
#include "seccomp-support.c"

#include <glib.h>
#include <glib/gstdio.h>

static void make_seccomp_profile(struct sc_seccomp_file_header *hdr, int *fd,
				 char **fname)
{
	*fd = g_file_open_tmp(NULL, fname, NULL);
	g_assert_true(*fd > 0);
	int written = write(*fd, hdr, sizeof(struct sc_seccomp_file_header));
	g_assert_true(written == sizeof(struct sc_seccomp_file_header));
}

static void test_must_read_and_validate_header_from_file__happy(void)
{
	struct sc_seccomp_file_header hdr = {
		.header[0] = 'S',
		.header[1] = 'C',
		.version = 1,
	};
	char SC_CLEANUP(sc_cleanup_string) * profile = NULL;
	int SC_CLEANUP(sc_cleanup_close) fd = 0;
	make_seccomp_profile(&hdr, &fd, &profile);

	FILE *file SC_CLEANUP(sc_cleanup_file) = fopen(profile, "rb");
	sc_must_read_and_validate_header_from_file(file, profile, &hdr);
	g_assert_true(file != NULL);
}

static void test_must_read_and_validate_header_from_file__missing_file(void)
{
	struct sc_seccomp_file_header hdr;
	const char *profile = "/path/to/missing/file";
	const char *expected_err =
	    "cannot open seccomp filter /path/to/missing/file: No such file or directory\n";

	if (g_test_subprocess()) {
		FILE *file SC_CLEANUP(sc_cleanup_file) = fopen(profile, "rb");
		sc_must_read_and_validate_header_from_file(file, profile, &hdr);
		// the function above is expected to call die()
		g_assert_not_reached();
		// reference "file" to keep the compiler from warning
		// that "file" is unused
		g_assert_null(file);
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr(expected_err);
}

static void must_read_and_validate_header_from_file_dies_with(struct
							      sc_seccomp_file_header
							      hdr, const char
							      *err_msg)
{
	if (g_test_subprocess()) {
		char SC_CLEANUP(sc_cleanup_string) * profile = NULL;
		int SC_CLEANUP(sc_cleanup_close) fd = 0;
		make_seccomp_profile(&hdr, &fd, &profile);

		FILE *file SC_CLEANUP(sc_cleanup_file) = fopen(profile, "rb");
		sc_must_read_and_validate_header_from_file(file, profile, &hdr);
		// the function above is expected to call die()
		g_assert_not_reached();
		// reference "file" to keep the compiler from warning
		// that "file" is unused
		g_assert_null(file);
	}

	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr(err_msg);
}

static void test_must_read_and_validate_header_from_file__invalid_header(void)
{
	//  when we stop supporting 14.04 we could just use hdr = {0}
	struct sc_seccomp_file_header hdr;
	memset(&hdr, 0, sizeof hdr);
	const char *expected_err = "unexpected seccomp header: 00\n";
	must_read_and_validate_header_from_file_dies_with(hdr, expected_err);
}

static void test_must_read_and_validate_header_from_file__invalid_version(void)
{
	struct sc_seccomp_file_header hdr = {
		.header[0] = 'S',
		.header[1] = 'C',
		.version = 0,
	};
	const char *expected_err = "unexpected seccomp file version: 0\n";
	must_read_and_validate_header_from_file_dies_with(hdr, expected_err);
}

static void
test_must_read_and_validate_header_from_file__len_allow_too_big(void)
{
	struct sc_seccomp_file_header hdr = {
		.header[0] = 'S',
		.header[1] = 'C',
		.version = 1,
		.len_allow_filter = MAX_BPF_SIZE + 1,
	};
	const char *expected_err = "allow filter size too big 32769\n";
	must_read_and_validate_header_from_file_dies_with(hdr, expected_err);
}

static void
test_must_read_and_validate_header_from_file__len_allow_no_multiplier(void)
{
	struct sc_seccomp_file_header hdr = {
		.header[0] = 'S',
		.header[1] = 'C',
		.version = 1,
		.len_allow_filter = sizeof(struct sock_filter) + 1,
	};
	const char *expected_err =
	    "allow filter size not multiple of sock_filter\n";
	must_read_and_validate_header_from_file_dies_with(hdr, expected_err);
}

static void test_must_read_and_validate_header_from_file__len_deny_too_big(void)
{
	struct sc_seccomp_file_header hdr = {
		.header[0] = 'S',
		.header[1] = 'C',
		.version = 1,
		.len_deny_filter = MAX_BPF_SIZE + 1,
	};
	const char *expected_err = "deny filter size too big 32769\n";
	must_read_and_validate_header_from_file_dies_with(hdr, expected_err);
}

static void
test_must_read_and_validate_header_from_file__len_deny_no_multiplier(void)
{
	struct sc_seccomp_file_header hdr = {
		.header[0] = 'S',
		.header[1] = 'C',
		.version = 1,
		.len_deny_filter = sizeof(struct sock_filter) + 1,
	};
	const char *expected_err =
	    "deny filter size not multiple of sock_filter\n";
	must_read_and_validate_header_from_file_dies_with(hdr, expected_err);
}

static void __attribute__((constructor)) init(void)
{
	g_test_add_func
	    ("/seccomp/must_read_and_validate_header_from_file/happy",
	     test_must_read_and_validate_header_from_file__happy);
	g_test_add_func
	    ("/seccomp/must_read_and_validate_header_from_file/missing_file",
	     test_must_read_and_validate_header_from_file__missing_file);
	g_test_add_func
	    ("/seccomp/must_read_and_validate_header_from_file/invalid_header",
	     test_must_read_and_validate_header_from_file__invalid_header);
	g_test_add_func
	    ("/seccomp/must_read_and_validate_header_from_file/invalid_version",
	     test_must_read_and_validate_header_from_file__invalid_version);
	g_test_add_func
	    ("/seccomp/must_read_and_validate_header_from_file/len_allow_too_big",
	     test_must_read_and_validate_header_from_file__len_allow_too_big);
	g_test_add_func
	    ("/seccomp/must_read_and_validate_header_from_file/len_allow_no_multiplier",
	     test_must_read_and_validate_header_from_file__len_allow_no_multiplier);
	g_test_add_func
	    ("/seccomp/must_read_and_validate_header_from_file/len_deny_too_big",
	     test_must_read_and_validate_header_from_file__len_deny_too_big);
	g_test_add_func
	    ("/seccomp/must_read_and_validate_header_from_file/len_deny_no_multiplier",
	     test_must_read_and_validate_header_from_file__len_deny_no_multiplier);
}
