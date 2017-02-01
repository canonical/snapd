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

#include <stdarg.h>

#include <glib.h>

/**
 * Create an argc + argv pair out of a NULL terminated argument list.
 **/
static void
    __attribute__ ((sentinel)) test_argc_argv(int *argcp, char ***argvp, ...)
{
	int argc = 0;
	char **argv = NULL;
	g_test_queue_free(argv);

	va_list ap;
	va_start(ap, argvp);
	const char *arg;
	do {
		arg = va_arg(ap, const char *);
		// XXX: yeah, wrong way but the worse that can happen is for test to fail
		argv = realloc(argv, sizeof(const char **) * (argc + 1));
		g_assert_nonnull(argv);
		if (arg != NULL) {
			char *arg_copy = strdup(arg);
			g_test_queue_free(arg_copy);
			argv[argc] = arg_copy;
			argc += 1;
		} else {
			argv[argc] = NULL;
		}
	} while (arg != NULL);
	va_end(ap);

	*argcp = argc;
	*argvp = argv;
}

static void test_test_argc_argv()
{
	// Check that test_argc_argv() correctly stores data
	int argc;
	char **argv;

	test_argc_argv(&argc, &argv, NULL);
	g_assert_cmpint(argc, ==, 0);
	g_assert_null(argv[0]);

	test_argc_argv(&argc, &argv, "zero", "one", "two", NULL);
	g_assert_cmpint(argc, ==, 3);
	g_assert_cmpstr(argv[0], ==, "zero");
	g_assert_cmpstr(argv[1], ==, "one");
	g_assert_cmpstr(argv[2], ==, "two");
	g_assert_null(argv[3]);
}

static void test_sc_nonfatal_parse_args__typical()
{
	// Test that typical invocation of snap-confine is parsed correctly.
	struct sc_error *err __attribute__ ((cleanup(sc_cleanup_error))) = NULL;
	struct sc_args *args __attribute__ ((cleanup(sc_cleanup_args))) = NULL;

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

	// Check remaining arguments
	g_assert_cmpint(argc, ==, 3);
	g_assert_cmpstr(argv[0], ==, "/usr/lib/snapd/snap-confine");
	g_assert_cmpstr(argv[1], ==, "--option");
	g_assert_cmpstr(argv[2], ==, "arg");
	g_assert_null(argv[3]);
}

static void test_sc_cleanup_args()
{
	// Check that NULL argument parser can be cleaned up
	struct sc_error *err __attribute__ ((cleanup(sc_cleanup_error))) = NULL;
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

static void test_sc_nonfatal_parse_args__typical_classic()
{
	// Test that typical invocation of snap-confine is parsed correctly.
	struct sc_error *err __attribute__ ((cleanup(sc_cleanup_error))) = NULL;
	struct sc_args *args __attribute__ ((cleanup(sc_cleanup_args))) = NULL;

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

static void test_sc_nonfatal_parse_args__ubuntu_core_launcher()
{
	// Test that typical legacy invocation of snap-confine via the
	// ubuntu-core-launcher symlink, with duplicated security tag, is parsed
	// correctly.
	struct sc_error *err __attribute__ ((cleanup(sc_cleanup_error))) = NULL;
	struct sc_args *args __attribute__ ((cleanup(sc_cleanup_args))) = NULL;

	int argc;
	char **argv;
	test_argc_argv(&argc, &argv,
		       "/usr/bin/ubuntu-core-launcher",
		       "snap.SNAP_NAME.APP_NAME", "snap.SNAP_NAME.APP_NAME",
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

	// Check remaining arguments
	g_assert_cmpint(argc, ==, 3);
	g_assert_cmpstr(argv[0], ==, "/usr/bin/ubuntu-core-launcher");
	g_assert_cmpstr(argv[1], ==, "--option");
	g_assert_cmpstr(argv[2], ==, "arg");
	g_assert_null(argv[3]);
}

static void test_sc_nonfatal_parse_args__version()
{
	// Test that snap-confine --version is detected.
	struct sc_error *err __attribute__ ((cleanup(sc_cleanup_error))) = NULL;
	struct sc_args *args __attribute__ ((cleanup(sc_cleanup_args))) = NULL;

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

static void test_sc_nonfatal_parse_args__evil_input()
{
	// Check that calling without any arguments is reported as error.
	struct sc_error *err __attribute__ ((cleanup(sc_cleanup_error))) = NULL;
	struct sc_args *args __attribute__ ((cleanup(sc_cleanup_args))) = NULL;

	// NULL argcp/argvp attack
	args = sc_nonfatal_parse_args(NULL, NULL, &err);

	g_assert_nonnull(err);
	g_assert_null(args);
	g_assert_cmpstr(sc_error_msg(err), ==,
			"cannot parse arguments, argcp or argvp is NULL");

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

static void test_sc_nonfatal_parse_args__nothing_to_parse()
{
	// Check that calling without any arguments is reported as error.
	struct sc_error *err __attribute__ ((cleanup(sc_cleanup_error))) = NULL;
	struct sc_args *args __attribute__ ((cleanup(sc_cleanup_args))) = NULL;

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

static void test_sc_nonfatal_parse_args__no_security_tag()
{
	// Check that lack of security tag is reported as error.
	struct sc_error *err __attribute__ ((cleanup(sc_cleanup_error))) = NULL;
	struct sc_args *args __attribute__ ((cleanup(sc_cleanup_args))) = NULL;

	int argc;
	char **argv;
	test_argc_argv(&argc, &argv, "/usr/lib/snapd/snap-confine", NULL);

	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	g_assert_nonnull(err);
	g_assert_null(args);

	// Check the error that we've got
	g_assert_cmpstr(sc_error_msg(err), ==,
			"application or hook security tag was not provided");
	g_assert_true(sc_error_match(err, SC_ARGS_DOMAIN, SC_ARGS_ERR_USAGE));
}

static void test_sc_nonfatal_parse_args__no_executable()
{
	// Check that lack of security tag is reported as error.
	struct sc_error *err __attribute__ ((cleanup(sc_cleanup_error))) = NULL;
	struct sc_args *args __attribute__ ((cleanup(sc_cleanup_args))) = NULL;

	int argc;
	char **argv;
	test_argc_argv(&argc, &argv, "/usr/lib/snapd/snap-confine",
		       "snap.SNAP_NAME.APP_NAME", NULL);

	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	g_assert_nonnull(err);
	g_assert_null(args);

	// Check the error that we've got
	g_assert_cmpstr(sc_error_msg(err), ==,
			"executable name was not provided");
	g_assert_true(sc_error_match(err, SC_ARGS_DOMAIN, SC_ARGS_ERR_USAGE));
}

static void test_sc_nonfatal_parse_args__unknown_option()
{
	// Check that unrecognized option switch is reported as error.
	struct sc_error *err __attribute__ ((cleanup(sc_cleanup_error))) = NULL;
	struct sc_args *args __attribute__ ((cleanup(sc_cleanup_args))) = NULL;

	int argc;
	char **argv;
	test_argc_argv(&argc, &argv, "/usr/lib/snapd/snap-confine",
		       "--frozbonicator", NULL);

	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	g_assert_nonnull(err);
	g_assert_null(args);

	// Check the error that we've got
	g_assert_cmpstr(sc_error_msg(err), ==,
			"unrecognized command line option: --frozbonicator");
	g_assert_true(sc_error_match(err, SC_ARGS_DOMAIN, SC_ARGS_ERR_USAGE));
}

static void test_sc_nonfatal_parse_args__forwards_error()
{
	// Check that sc_nonfatal_parse_args() forwards errors.
	if (g_test_subprocess()) {
		int argc;
		char **argv;
		test_argc_argv(&argc, &argv, "/usr/lib/snapd/snap-confine",
			       "--frozbonicator", NULL);

		// Call sc_nonfatal_parse_args() without an error handle
		struct sc_args *args
		    __attribute__ ((cleanup(sc_cleanup_args))) = NULL;
		args = sc_nonfatal_parse_args(&argc, &argv, NULL);
		(void)args;

		g_test_message("expected not to reach this place");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr
	    ("unrecognized command line option: --frozbonicator\n");
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/args/test_argc_argv", test_test_argc_argv);
	g_test_add_func("/args/sc_cleanup_args", test_sc_cleanup_args);
	g_test_add_func("/args/sc_nonfatal_parse_args/typical",
			test_sc_nonfatal_parse_args__typical);
	g_test_add_func("/args/sc_nonfatal_parse_args/typical_classic",
			test_sc_nonfatal_parse_args__typical_classic);
	g_test_add_func("/args/sc_nonfatal_parse_args/ubuntu_core_launcher",
			test_sc_nonfatal_parse_args__ubuntu_core_launcher);
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
}
