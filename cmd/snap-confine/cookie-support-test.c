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

#include "cookie-support.h"
#include "cookie-support.c"

#include "../libsnap-confine-private/test-utils.h"

#include <glib.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

// Set alternate cookie directory
static void set_cookie_dir(const char *dir)
{
	sc_cookie_dir = dir;
}

static void set_fake_cookie_dir(void)
{
	char *ctx_dir = NULL;
	ctx_dir = g_dir_make_tmp(NULL, NULL);
	g_assert_nonnull(ctx_dir);
	g_test_queue_free(ctx_dir);

	g_test_queue_destroy((GDestroyNotify) rm_rf_tmp, ctx_dir);
	g_test_queue_destroy((GDestroyNotify) set_cookie_dir, SC_COOKIE_DIR);

	set_cookie_dir(ctx_dir);
}

static void create_dumy_cookie_file(const char *snap_name,
				    const char *dummy_cookie)
{
	char path[PATH_MAX] = { 0 };
	FILE *f;
	int n;

	snprintf(path, sizeof(path), "%s/snap.%s", sc_cookie_dir, snap_name);

	f = fopen(path, "w");
	g_assert_nonnull(f);

	n = fwrite(dummy_cookie, 1, strlen(dummy_cookie), f);
	g_assert_cmpint(n, ==, strlen(dummy_cookie));

	fclose(f);
}

static void test_cookie_get_from_snapd__successful(void)
{
	struct sc_error *err = NULL;
	char *cookie;

	char *dummy = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijmnopqrst";

	set_fake_cookie_dir();
	create_dumy_cookie_file("test-snap", dummy);

	cookie = sc_cookie_get_from_snapd("test-snap", &err);
	g_assert_null(err);
	g_assert_nonnull(cookie);
	g_assert_cmpint(strlen(cookie), ==, 44);
	g_assert_cmpstr(cookie, ==, dummy);
}

static void test_cookie_get_from_snapd__nofile(void)
{
	struct sc_error *err = NULL;
	char *cookie;

	set_fake_cookie_dir();

	cookie = sc_cookie_get_from_snapd("test-snap2", &err);
	g_assert_nonnull(err);
	g_assert_cmpstr(sc_error_domain(err), ==, SC_ERRNO_DOMAIN);
	g_assert_nonnull(strstr(sc_error_msg(err), "cannot open cookie file"));
	g_assert_null(cookie);
}

static void __attribute__ ((constructor)) init(void)
{
	g_test_add_func("/snap-cookie/cookie_get_from_snapd/successful",
			test_cookie_get_from_snapd__successful);
	g_test_add_func("/snap-cookie/cookie_get_from_snapd/no_cookie_file",
			test_cookie_get_from_snapd__nofile);
}
