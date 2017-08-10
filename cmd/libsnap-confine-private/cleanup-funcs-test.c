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

#include "cleanup-funcs.h"
#include "cleanup-funcs.c"

#include <glib.h>

// Test that cleanup functions are applied as expected
static void test_cleanup_sanity()
{
	int called = 0;
	void fn(int *ptr) {
		called = 1;
	}
	{
		int test __attribute__ ((cleanup(fn)));
		test = 0;
		test++;
	}
	g_assert_cmpint(called, ==, 1);
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/cleanup/sanity", test_cleanup_sanity);
}
