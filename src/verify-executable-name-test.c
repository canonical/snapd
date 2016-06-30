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

#include "snap.h"
#include "snap.c"

#include <glib.h>

static void test_verify_executable_name()
{
	// First, test the names we know are good
	g_assert_true(verify_executable_name("snap.name.app"));
	g_assert_true(verify_executable_name
		      ("snap.network-manager.NetworkManager"));
	g_assert_true(verify_executable_name("snap.f00.bar-baz1"));
	g_assert_true(verify_executable_name("snap.foo.hook.bar"));
	g_assert_true(verify_executable_name("snap.foo.hook.bar-baz"));

	// Now, test the names we know are bad
	g_assert_false(verify_executable_name("pkg-foo.bar.0binary-bar+baz"));
	g_assert_false(verify_executable_name("pkg-foo_bar_1.1"));
	g_assert_false(verify_executable_name("appname/.."));
	g_assert_false(verify_executable_name("snap"));
	g_assert_false(verify_executable_name("snap."));
	g_assert_false(verify_executable_name("snap.name."));
	g_assert_false(verify_executable_name("snap.name.app."));
	g_assert_false(verify_executable_name("snap.name.hook."));
	g_assert_false(verify_executable_name("snap!name.app"));
	g_assert_false(verify_executable_name("snap.-name.app"));
	g_assert_false(verify_executable_name("snap.name!app"));
	g_assert_false(verify_executable_name("snap.name.-app"));
	g_assert_false(verify_executable_name("snap.name.app!hook.foo"));
	g_assert_false(verify_executable_name("snap.name.app.hook!foo"));
	g_assert_false(verify_executable_name("snap.name.app.hook.-foo"));
	g_assert_false(verify_executable_name("snap.name.app.hook.f00"));
	g_assert_false(verify_executable_name("sna.pname.app"));
	g_assert_false(verify_executable_name("snap.n@me.app"));
	g_assert_false(verify_executable_name("SNAP.name.app"));
	g_assert_false(verify_executable_name("snap.Name.app"));
	g_assert_false(verify_executable_name("snap.0name.app"));
	g_assert_false(verify_executable_name("snap.-name.app"));
	g_assert_false(verify_executable_name("snap.name.@app"));
	g_assert_false(verify_executable_name(".name.app"));
	g_assert_false(verify_executable_name("snap..name.app"));
	g_assert_false(verify_executable_name("snap.name..app"));
	g_assert_false(verify_executable_name("snap.name.app.."));
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/snap/verify_executable_name",
			test_verify_executable_name);
}
