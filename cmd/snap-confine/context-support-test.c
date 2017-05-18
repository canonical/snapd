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

#include <glib.h>
#include <stdlib.h>

static void test_maybe_set_context_environment__null()
{
  setenv("SNAP_CONTEXT", "bar", 1);
  sc_maybe_set_context_environment(NULL);
  g_assert_cmpstr(getenv("SNAP_CONTEXT"), ==, "bar");
}

static void test_maybe_set_context_environment__overwrite()
{
  setenv("SNAP_CONTEXT", "bar", 1);
  sc_maybe_set_context_environment("foo");
  g_assert_cmpstr(getenv("SNAP_CONTEXT"), ==, "foo");
}

static void test_maybe_set_context_environment__typical()
{
  setenv("SNAP_CONTEXT", "bar", 1);
  sc_maybe_set_context_environment("foo");
  g_assert_cmpstr(getenv("SNAP_CONTEXT"), ==, "foo");
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/snap-context/set_context_environment_maybe/null_context", test_maybe_set_context_environment__null);
  g_test_add_func("/snap-context/set_context_environment_maybe/overwrite", test_maybe_set_context_environment__overwrite);
  g_test_add_func("/snap-context/set_context_environment_maybe/typical", test_maybe_set_context_environment__typical);
}
