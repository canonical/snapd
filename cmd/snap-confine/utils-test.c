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

static void test_str2bool()
{
	int err;
	bool value;

	err = str2bool("yes", &value);
	g_assert_cmpint(err, ==, 0);
	g_assert_true(value);

	err = str2bool("1", &value);
	g_assert_cmpint(err, ==, 0);
	g_assert_true(value);

	err = str2bool("no", &value);
	g_assert_cmpint(err, ==, 0);
	g_assert_false(value);

	err = str2bool("0", &value);
	g_assert_cmpint(err, ==, 0);
	g_assert_false(value);

	err = str2bool("", &value);
	g_assert_cmpint(err, ==, 0);
	g_assert_false(value);

	err = str2bool(NULL, &value);
	g_assert_cmpint(err, ==, 0);
	g_assert_false(value);

	err = str2bool("flower", &value);
	g_assert_cmpint(err, ==, -1);
	g_assert_cmpint(errno, ==, EINVAL);

	err = str2bool("yes", NULL);
	g_assert_cmpint(err, ==, -1);
	g_assert_cmpint(errno, ==, EFAULT);
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/utils/str2bool", test_str2bool);
}
