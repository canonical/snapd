/*
 * Copyright (C) 2017 Canonical Ltd
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

#include "context-support.h"
#include "context-support.c"

#include "../libsnap-confine-private/test-utils.h"

#include <glib.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

// Set alternate context directory
static void set_context_dir(const char *dir)
{
	sc_context_dir = dir;
}

static void set_fake_context_dir()
{
	char *ctx_dir = NULL;
	ctx_dir = g_dir_make_tmp(NULL, NULL);
	g_assert_nonnull(ctx_dir);
	g_test_queue_free(ctx_dir);

	g_test_queue_destroy((GDestroyNotify) rm_rf_tmp, ctx_dir);
	g_test_queue_destroy((GDestroyNotify) set_context_dir, SC_CONTEXT_DIR);

	set_context_dir(ctx_dir);
}

static void create_dumy_context_file(const char *snap_name,
				     const char *dummy_context)
{
	char path[256];
	FILE *f;
	int n;

	snprintf(path, sizeof(path), "%s/snap.%s", sc_context_dir, snap_name);

	f = fopen(path, "w");
	g_assert_nonnull(f);

	n = fwrite(dummy_context, 1, strlen(dummy_context), f);
	g_assert_cmpint(n, ==, strlen(dummy_context));

	fclose(f);
}

static void test_maybe_set_context_environment__null()
{
	if (g_test_subprocess()) {
		setenv("SNAP_CONTEXT", "bar", 1);
		sc_maybe_set_context_environment(NULL);
		g_assert_cmpstr(getenv("SNAP_CONTEXT"), ==, "bar");
		return;
	}

	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_passed();
}

static void test_maybe_set_context_environment__overwrite()
{
	if (g_test_subprocess()) {
		setenv("SNAP_CONTEXT", "bar", 1);
		sc_maybe_set_context_environment("foo");
		g_assert_cmpstr(getenv("SNAP_CONTEXT"), ==, "foo");
		return;
	}

	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_passed();
}

static void test_maybe_set_context_environment__typical()
{
	if (g_test_subprocess()) {
    setenv("SNAP_CONTEXT", "bar", 1);
    sc_maybe_set_context_environment("foo");
    g_assert_cmpstr(getenv("SNAP_CONTEXT"), ==, "foo");
    return;
  }

	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_passed();
}

static void test_context_get_from_snapd__successful()
{
	struct sc_error *err;
	char *context;

	char *dummy = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijmnopqrst";

	set_fake_context_dir();
	create_dumy_context_file("test-snap", dummy);

	context = sc_context_get_from_snapd("test-snap", &err);
	g_assert_null(err);
	g_assert_nonnull(context);
	g_assert_cmpint(strlen(context), ==, 44);
	g_assert_cmpstr(context, ==, dummy);
}

static void test_context_get_from_snapd__nofile()
{
	struct sc_error *err;
	char *context;

	set_fake_context_dir();

	context = sc_context_get_from_snapd("test-snap2", &err);
	g_assert_nonnull(err);
	g_assert_cmpstr(sc_error_domain(err), ==, SC_ERRNO_DOMAIN);
	g_assert_nonnull(strstr(sc_error_msg(err), "cannot open context file"));
	g_assert_null(context);
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func
	    ("/snap-context/set_context_environment_maybe/null_context",
	     test_maybe_set_context_environment__null);
	g_test_add_func("/snap-context/set_context_environment_maybe/overwrite",
			test_maybe_set_context_environment__overwrite);
	g_test_add_func("/snap-context/set_context_environment_maybe/typical",
			test_maybe_set_context_environment__typical);
	g_test_add_func("/snap-context/context_get_from_snapd/successful",
			test_context_get_from_snapd__successful);
	g_test_add_func("/snap-context/context_get_from_snapd/no_context_file",
			test_context_get_from_snapd__nofile);
}
