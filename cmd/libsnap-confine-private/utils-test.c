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

#include "utils.h"
#include "utils.c"

#include <glib.h>

static void test_parse_bool(void)
{
	int err;
	bool value;

	value = false;
	err = parse_bool("yes", &value, false);
	g_assert_cmpint(err, ==, 0);
	g_assert_true(value);

	value = false;
	err = parse_bool("1", &value, false);
	g_assert_cmpint(err, ==, 0);
	g_assert_true(value);

	value = true;
	err = parse_bool("no", &value, false);
	g_assert_cmpint(err, ==, 0);
	g_assert_false(value);

	value = true;
	err = parse_bool("0", &value, false);
	g_assert_cmpint(err, ==, 0);
	g_assert_false(value);

	value = true;
	err = parse_bool("", &value, false);
	g_assert_cmpint(err, ==, 0);
	g_assert_false(value);

	value = true;
	err = parse_bool(NULL, &value, false);
	g_assert_cmpint(err, ==, 0);
	g_assert_false(value);

	value = false;
	err = parse_bool(NULL, &value, true);
	g_assert_cmpint(err, ==, 0);
	g_assert_true(value);

	value = true;
	err = parse_bool("flower", &value, false);
	g_assert_cmpint(err, ==, -1);
	g_assert_cmpint(errno, ==, EINVAL);
	g_assert_true(value);

	err = parse_bool("yes", NULL, false);
	g_assert_cmpint(err, ==, -1);
	g_assert_cmpint(errno, ==, EFAULT);
}

static void test_sc_is_expected_path(void)
{
	struct {
		const char *path;
		bool expected;
	} test_cases[] = {
		{"/tmp/snap-confine", false},
		{"/tmp/foo", false},
		{"/home/ ", false},
		{"/usr/lib/snapd/snap-confine1", false},
		{"/usr/lib/snapd/snap—confine", false},
		{"/snap/core/usr/lib/snapd/snap-confine", false},
		{"/snap/core/x1x/usr/lib/snapd/snap-confine", false},
		{"/snap/core/z1/usr/lib/snapd/snap-confine", false},
		{"/snap/cꓳre/1/usr/lib/snapd/snap-confine", false},
		{"/snap/snapd1/1/usr/lib/snapd/snap-confine", false},
		{"/snap/core/current/usr/lib/snapd/snap-confine", false},
		{"/usr/lib/snapd/snap-confine", true},
		{"/usr/libexec/snapd/snap-confine", true},
		{"/snap/core/1/usr/lib/snapd/snap-confine", true},
		{"/snap/core/x1/usr/lib/snapd/snap-confine", true},
		{"/snap/snapd/1/usr/lib/snapd/snap-confine", true},
		{"/snap/snapd/1/usr/libexec/snapd/snap-confine", false},
	};
	size_t i;
	for (i = 0; i < sizeof(test_cases) / sizeof(test_cases[0]); i++) {
		bool result = sc_is_expected_path(test_cases[i].path);
		g_assert_cmpint(result, ==, test_cases[i].expected);
	}
}

static void test_die(void)
{
	if (g_test_subprocess()) {
		errno = 0;
		die("death message");
		g_test_message("expected die not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr("death message\n");
}

static void test_die_with_errno(void)
{
	if (g_test_subprocess()) {
		errno = EPERM;
		die("death message");
		g_test_message("expected die not to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr("death message: Operation not permitted\n");
}

// A variant of rmdir that is compatible with GDestroyNotify
static void my_rmdir(const char *path)
{
	if (rmdir(path) != 0) {
		die("cannot rmdir %s", path);
	}
}

// A variant of chdir that is compatible with GDestroyNotify
static void my_chdir(const char *path)
{
	if (chdir(path) != 0) {
		die("cannot change dir to %s", path);
	}
}

/**
 * Perform the rest of testing in a ephemeral directory.
 *
 * Create a temporary directory, move the current process there and undo those
 * operations at the end of the test.  If any additional directories or files
 * are created in this directory they must be removed by the caller.
 **/
static void g_test_in_ephemeral_dir(void)
{
	gchar *temp_dir = g_dir_make_tmp(NULL, NULL);
	gchar *orig_dir = g_get_current_dir();
	int err = chdir(temp_dir);
	g_assert_cmpint(err, ==, 0);

	g_test_queue_free(temp_dir);
	g_test_queue_destroy((GDestroyNotify) my_rmdir, temp_dir);
	g_test_queue_free(orig_dir);
	g_test_queue_destroy((GDestroyNotify) my_chdir, orig_dir);
}

/**
 * Test sc_nonfatal_mkpath() given two directories.
 **/
static void _test_sc_nonfatal_mkpath(const gchar *dirname,
				     const gchar *subdirname)
{
	// Check that directory does not exist.
	g_assert_false(g_file_test(dirname, G_FILE_TEST_EXISTS |
				   G_FILE_TEST_IS_DIR));
	// Use sc_nonfatal_mkpath to create the directory and ensure that it worked
	// as expected.
	g_test_queue_destroy((GDestroyNotify) my_rmdir, (char *)dirname);
	int err = sc_nonfatal_mkpath(dirname, 0755);
	g_assert_cmpint(err, ==, 0);
	g_assert_cmpint(errno, ==, 0);
	g_assert_true(g_file_test(dirname, G_FILE_TEST_EXISTS |
				  G_FILE_TEST_IS_REGULAR));
	// Use same function again to try to create the same directory and ensure
	// that it didn't fail and properly retained EEXIST in errno.
	err = sc_nonfatal_mkpath(dirname, 0755);
	g_assert_cmpint(err, ==, 0);
	g_assert_cmpint(errno, ==, EEXIST);
	// Now create a sub-directory of the original directory and observe the
	// results. We should no longer see errno of EEXIST!
	g_test_queue_destroy((GDestroyNotify) my_rmdir, (char *)subdirname);
	err = sc_nonfatal_mkpath(subdirname, 0755);
	g_assert_cmpint(err, ==, 0);
	g_assert_cmpint(errno, ==, 0);
}

/**
 * Test that sc_nonfatal_mkpath behaves when using relative paths.
 **/
static void test_sc_nonfatal_mkpath__relative(void)
{
	g_test_in_ephemeral_dir();
	gchar *current_dir = g_get_current_dir();
	g_test_queue_free(current_dir);
	gchar *dirname = g_build_path("/", current_dir, "foo", NULL);
	g_test_queue_free(dirname);
	gchar *subdirname = g_build_path("/", current_dir, "foo", "bar", NULL);
	g_test_queue_free(subdirname);
	_test_sc_nonfatal_mkpath(dirname, subdirname);
}

/**
 * Test that sc_nonfatal_mkpath behaves when using absolute paths.
 **/
static void test_sc_nonfatal_mkpath__absolute(void)
{
	g_test_in_ephemeral_dir();
	const char *dirname = "foo";
	const char *subdirname = "foo/bar";
	_test_sc_nonfatal_mkpath(dirname, subdirname);
}

static void __attribute__((constructor)) init(void)
{
	g_test_add_func("/utils/parse_bool", test_parse_bool);
	g_test_add_func("/utils/sc_is_expected_path", test_sc_is_expected_path);
	g_test_add_func("/utils/die", test_die);
	g_test_add_func("/utils/die_with_errno", test_die_with_errno);
	g_test_add_func("/utils/sc_nonfatal_mkpath/relative",
			test_sc_nonfatal_mkpath__relative);
	g_test_add_func("/utils/sc_nonfatal_mkpath/absolute",
			test_sc_nonfatal_mkpath__absolute);
}
