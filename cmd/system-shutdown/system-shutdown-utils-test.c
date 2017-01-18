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

#include "system-shutdown-utils.h"
#include "system-shutdown-utils.c"

#include <glib.h>

static void test_streq()
{
	g_assert_false(streq(NULL, NULL));
	g_assert_false(streq(NULL, "text"));
	g_assert_false(streq("text", NULL));
	g_assert_false(streq("foo", "bar"));
	g_assert_false(streq("foo", "barbar"));
	g_assert_false(streq("foofoo", "bar"));
	g_assert_true(streq("text", "text"));
	g_assert_true(streq("", ""));
}

static void test_endswith()
{
	g_assert_false(endswith("", NULL));
	g_assert_false(endswith(NULL, ""));
	g_assert_false(endswith(NULL, NULL));
	g_assert_true(endswith("", ""));
	g_assert_true(endswith("foobar", "bar"));
	g_assert_true(endswith("foobar", "ar"));
	g_assert_true(endswith("foobar", "r"));
	g_assert_true(endswith("foobar", ""));
	g_assert_false(endswith("foobar", "quux"));
	g_assert_false(endswith("", "bar"));
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/system_shutdown/streq", test_streq);
	g_test_add_func("/system_shutdown/endswith", test_endswith);
}
