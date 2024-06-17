/*
 * Copyright (C) 2016 Canonical Ltd
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

#include "snap-confine-args.h"
#include "snap-confine-args.c"
#include "../libsnap-confine-private/cleanup-funcs.h"

#include <stdarg.h>

#include <glib.h>

static void test_sc_nonfatal_parse_args__typical(void)
{
	// Test that typical invocation of snap-confine is parsed correctly.
	sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
	struct sc_args *args SC_CLEANUP(sc_cleanup_args) = NULL;

	int argc;
	char **argv;
	test_argc_argv(&argc, &argv,
		       "/usr/lib/snapd/snap-confine", "snap.SNAP_NAME.APP_NAME",
		       "/usr/lib/snapd/snap-exec", "--option", "arg", NULL);

	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	g_assert_null(err);
	g_assert_nonnull(args);

	// Check supported switches and arguments
	g_assert_cmpstr(sc_args_security_tag(args), ==,
			"snap.SNAP_NAME.APP_NAME");
	g_assert_cmpstr(sc_args_executable(args), ==,
			"/usr/lib/snapd/snap-exec");
	g_assert_cmpint(sc_args_is_version_query(args), ==, false);
	g_assert_cmpint(sc_args_is_classic_confinement(args), ==, false);
	g_assert_null(sc_args_base_snap(args));

	// Check remaining arguments
	g_assert_cmpint(argc, ==, 3);
	g_assert_cmpstr(argv[0], ==, "/usr/lib/snapd/snap-confine");
	g_assert_cmpstr(argv[1], ==, "--option");
	g_assert_cmpstr(argv[2], ==, "arg");
	g_assert_null(argv[3]);
}

static void test_sc_cleanup_args(void)
{
	// Check that NULL argument parser can be cleaned up
	sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
	struct sc_args *args = NULL;
	sc_cleanup_args(&args);

	// Check that a non-NULL argument parser can be cleaned up
	int argc;
	char **argv;
	test_argc_argv(&argc, &argv, "/usr/lib/snapd/snap-confine",
		       "snap.SNAP_NAME.APP_NAME", "/usr/lib/snapd/snap-exec",
		       NULL);
	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	g_assert_null(err);
	g_assert_nonnull(args);

	sc_cleanup_args(&args);
	g_assert_null(args);
}

static void test_sc_nonfatal_parse_args__typical_classic(void)
{
	// Test that typical invocation of snap-confine is parsed correctly.
	sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
	struct sc_args *args SC_CLEANUP(sc_cleanup_args) = NULL;

	int argc;
	char **argv;
	test_argc_argv(&argc, &argv,
		       "/usr/lib/snapd/snap-confine", "--classic",
		       "snap.SNAP_NAME.APP_NAME", "/usr/lib/snapd/snap-exec",
		       "--option", "arg", NULL);

	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	g_assert_null(err);
	g_assert_nonnull(args);

	// Check supported switches and arguments
	g_assert_cmpstr(sc_args_security_tag(args), ==,
			"snap.SNAP_NAME.APP_NAME");
	g_assert_cmpstr(sc_args_executable(args), ==,
			"/usr/lib/snapd/snap-exec");
	g_assert_cmpint(sc_args_is_version_query(args), ==, false);
	g_assert_cmpint(sc_args_is_classic_confinement(args), ==, true);

	// Check remaining arguments
	g_assert_cmpint(argc, ==, 3);
	g_assert_cmpstr(argv[0], ==, "/usr/lib/snapd/snap-confine");
	g_assert_cmpstr(argv[1], ==, "--option");
	g_assert_cmpstr(argv[2], ==, "arg");
	g_assert_null(argv[3]);
}

static void test_sc_nonfatal_parse_args__version(void)
{
	// Test that snap-confine --version is detected.
	sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
	struct sc_args *args SC_CLEANUP(sc_cleanup_args) = NULL;

	int argc;
	char **argv;
	test_argc_argv(&argc, &argv,
		       "/usr/lib/snapd/snap-confine", "--version", "ignored",
		       "garbage", NULL);

	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	g_assert_null(err);
	g_assert_nonnull(args);

	// Check supported switches and arguments
	g_assert_null(sc_args_security_tag(args));
	g_assert_null(sc_args_executable(args));
	g_assert_cmpint(sc_args_is_version_query(args), ==, true);
	g_assert_cmpint(sc_args_is_classic_confinement(args), ==, false);

	// Check remaining arguments
	g_assert_cmpint(argc, ==, 3);
	g_assert_cmpstr(argv[0], ==, "/usr/lib/snapd/snap-confine");
	g_assert_cmpstr(argv[1], ==, "ignored");
	g_assert_cmpstr(argv[2], ==, "garbage");
	g_assert_null(argv[3]);
}

static void test_sc_nonfatal_parse_args__evil_input(void)
{
	// Check that calling without any arguments is reported as error.
	sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
	struct sc_args *args SC_CLEANUP(sc_cleanup_args) = NULL;

	// NULL argcp/argvp attack
	args = sc_nonfatal_parse_args(NULL, NULL, &err);

	g_assert_nonnull(err);
	g_assert_null(args);
	g_assert_cmpstr(sc_error_msg(err), ==,
			"cannot parse arguments, argcp or argvp is NULL");
	sc_cleanup_error(&err);

	int argc;
	char **argv;

	// NULL argv attack
	argc = 0;
	argv = NULL;
	args = sc_nonfatal_parse_args(&argc, &argv, &err);

	g_assert_nonnull(err);
	g_assert_null(args);
	g_assert_cmpstr(sc_error_msg(err), ==,
			"cannot parse arguments, argc is zero or argv is NULL");
	sc_cleanup_error(&err);

	// NULL argv[i] attack
	test_argc_argv(&argc, &argv,
		       "/usr/lib/snapd/snap-confine", "--version", "ignored",
		       "garbage", NULL);
	argv[1] = NULL;		// overwrite --version with NULL
	args = sc_nonfatal_parse_args(&argc, &argv, &err);

	g_assert_nonnull(err);
	g_assert_null(args);
	g_assert_cmpstr(sc_error_msg(err), ==,
			"cannot parse arguments, argument at index 1 is NULL");
}

static void test_sc_nonfatal_parse_args__nothing_to_parse(void)
{
	// Check that calling without any arguments is reported as error.
	sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
	struct sc_args *args SC_CLEANUP(sc_cleanup_args) = NULL;

	int argc;
	char **argv;
	test_argc_argv(&argc, &argv, NULL);

	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	g_assert_nonnull(err);
	g_assert_null(args);

	// Check the error that we've got
	g_assert_cmpstr(sc_error_msg(err), ==,
			"cannot parse arguments, argc is zero or argv is NULL");
}

static void test_sc_nonfatal_parse_args__no_security_tag(void)
{
	// Check that lack of security tag is reported as error.
	sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
	struct sc_args *args SC_CLEANUP(sc_cleanup_args) = NULL;

	int argc;
	char **argv;
	test_argc_argv(&argc, &argv, "/usr/lib/snapd/snap-confine", NULL);

	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	g_assert_nonnull(err);
	g_assert_null(args);

	// Check the error that we've got
	g_assert_cmpstr(sc_error_msg(err), ==,
			"Usage: snap-confine <security-tag> <executable>\n"
			"\napplication or hook security tag was not provided");

	g_assert_true(sc_error_match(err, SC_ARGS_DOMAIN, SC_ARGS_ERR_USAGE));
}

static void test_sc_nonfatal_parse_args__no_executable(void)
{
	// Check that lack of security tag is reported as error.
	sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
	struct sc_args *args SC_CLEANUP(sc_cleanup_args) = NULL;

	int argc;
	char **argv;
	test_argc_argv(&argc, &argv, "/usr/lib/snapd/snap-confine",
		       "snap.SNAP_NAME.APP_NAME", NULL);

	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	g_assert_nonnull(err);
	g_assert_null(args);

	// Check the error that we've got
	g_assert_cmpstr(sc_error_msg(err), ==,
			"Usage: snap-confine <security-tag> <executable>\n"
			"\nexecutable name was not provided");
	g_assert_true(sc_error_match(err, SC_ARGS_DOMAIN, SC_ARGS_ERR_USAGE));
}

static void test_sc_nonfatal_parse_args__unknown_option(void)
{
	// Check that unrecognized option switch is reported as error.
	sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
	struct sc_args *args SC_CLEANUP(sc_cleanup_args) = NULL;

	int argc;
	char **argv;
	test_argc_argv(&argc, &argv, "/usr/lib/snapd/snap-confine",
		       "--frozbonicator", NULL);

	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	g_assert_nonnull(err);
	g_assert_null(args);

	// Check the error that we've got
	g_assert_cmpstr(sc_error_msg(err), ==,
			"Usage: snap-confine <security-tag> <executable>\n"
			"\nunrecognized command line option: --frozbonicator");
	g_assert_true(sc_error_match(err, SC_ARGS_DOMAIN, SC_ARGS_ERR_USAGE));
}

static void test_sc_nonfatal_parse_args__forwards_error(void)
{
	// Check that sc_nonfatal_parse_args() forwards errors.
	if (g_test_subprocess()) {
		int argc;
		char **argv;
		test_argc_argv(&argc, &argv, "/usr/lib/snapd/snap-confine",
			       "--frozbonicator", NULL);

		// Call sc_nonfatal_parse_args() without an error handle
		struct sc_args *args SC_CLEANUP(sc_cleanup_args) = NULL;
		args = sc_nonfatal_parse_args(&argc, &argv, NULL);
		(void)args;

		g_test_message("expected not to reach this place");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("Usage: snap-confine <security-tag> <executable>\n"
	     "\nunrecognized command line option: --frozbonicator\n");
}

static void test_sc_nonfatal_parse_args__base_snap(void)
{
	// Check that --base specifies the name of the base snap.
	sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
	struct sc_args *args SC_CLEANUP(sc_cleanup_args) = NULL;

	int argc;
	char **argv;
	test_argc_argv(&argc, &argv,
		       "/usr/lib/snapd/snap-confine", "--base", "base-snap",
		       "snap.SNAP_NAME.APP_NAME", "/usr/lib/snapd/snap-exec",
		       NULL);

	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	g_assert_null(err);
	g_assert_nonnull(args);

	// Check the --base switch
	g_assert_cmpstr(sc_args_base_snap(args), ==, "base-snap");
	// Check other arguments
	g_assert_cmpstr(sc_args_security_tag(args), ==,
			"snap.SNAP_NAME.APP_NAME");
	g_assert_cmpstr(sc_args_executable(args), ==,
			"/usr/lib/snapd/snap-exec");
	g_assert_cmpint(sc_args_is_version_query(args), ==, false);
	g_assert_cmpint(sc_args_is_classic_confinement(args), ==, false);
}

static void test_sc_nonfatal_parse_args__base_snap__missing_arg(void)
{
	// Check that --base specifies the name of the base snap.
	sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
	struct sc_args *args SC_CLEANUP(sc_cleanup_args) = NULL;

	int argc;
	char **argv;
	test_argc_argv(&argc, &argv,
		       "/usr/lib/snapd/snap-confine", "--base", NULL);

	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	g_assert_nonnull(err);
	g_assert_null(args);

	// Check the error that we've got
	g_assert_cmpstr(sc_error_msg(err), ==,
			"Usage: snap-confine <security-tag> <executable>\n"
			"\nthe --base option requires an argument");
	g_assert_true(sc_error_match(err, SC_ARGS_DOMAIN, SC_ARGS_ERR_USAGE));
}

static void test_sc_nonfatal_parse_args__base_snap__twice(void)
{
	// Check that --base specifies the name of the base snap.
	sc_error *err SC_CLEANUP(sc_cleanup_error) = NULL;
	struct sc_args *args SC_CLEANUP(sc_cleanup_args) = NULL;

	int argc;
	char **argv;
	test_argc_argv(&argc, &argv,
		       "/usr/lib/snapd/snap-confine",
		       "--base", "base1", "--base", "base2", NULL);

	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	g_assert_nonnull(err);
	g_assert_null(args);

	// Check the error that we've got
	g_assert_cmpstr(sc_error_msg(err), ==,
			"Usage: snap-confine <security-tag> <executable>\n"
			"\nthe --base option can be used only once");
	g_assert_true(sc_error_match(err, SC_ARGS_DOMAIN, SC_ARGS_ERR_USAGE));
}

static void __attribute__((constructor)) init(void)
{
	g_test_add_func("/args/sc_cleanup_args", test_sc_cleanup_args);
	g_test_add_func("/args/sc_nonfatal_parse_args/typical",
			test_sc_nonfatal_parse_args__typical);
	g_test_add_func("/args/sc_nonfatal_parse_args/typical_classic",
			test_sc_nonfatal_parse_args__typical_classic);
	g_test_add_func("/args/sc_nonfatal_parse_args/version",
			test_sc_nonfatal_parse_args__version);
	g_test_add_func("/args/sc_nonfatal_parse_args/nothing_to_parse",
			test_sc_nonfatal_parse_args__nothing_to_parse);
	g_test_add_func("/args/sc_nonfatal_parse_args/evil_input",
			test_sc_nonfatal_parse_args__evil_input);
	g_test_add_func("/args/sc_nonfatal_parse_args/no_security_tag",
			test_sc_nonfatal_parse_args__no_security_tag);
	g_test_add_func("/args/sc_nonfatal_parse_args/no_executable",
			test_sc_nonfatal_parse_args__no_executable);
	g_test_add_func("/args/sc_nonfatal_parse_args/unknown_option",
			test_sc_nonfatal_parse_args__unknown_option);
	g_test_add_func("/args/sc_nonfatal_parse_args/forwards_error",
			test_sc_nonfatal_parse_args__forwards_error);
	g_test_add_func("/args/sc_nonfatal_parse_args/base_snap",
			test_sc_nonfatal_parse_args__base_snap);
	g_test_add_func("/args/sc_nonfatal_parse_args/base_snap/missing-arg",
			test_sc_nonfatal_parse_args__base_snap__missing_arg);
	g_test_add_func("/args/sc_nonfatal_parse_args/base_snap/twice",
			test_sc_nonfatal_parse_args__base_snap__twice);
}
