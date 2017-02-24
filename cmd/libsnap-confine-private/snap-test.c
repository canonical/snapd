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

#include "snap.h"
#include "snap.c"

#include <glib.h>

static void test_verify_security_tag()
{
	// First, test the names we know are good
	g_assert_true(verify_security_tag("snap.name.app"));
	g_assert_true(verify_security_tag
		      ("snap.network-manager.NetworkManager"));
	g_assert_true(verify_security_tag("snap.f00.bar-baz1"));
	g_assert_true(verify_security_tag("snap.foo.hook.bar"));
	g_assert_true(verify_security_tag("snap.foo.hook.bar-baz"));

	// Now, test the names we know are bad
	g_assert_false(verify_security_tag("pkg-foo.bar.0binary-bar+baz"));
	g_assert_false(verify_security_tag("pkg-foo_bar_1.1"));
	g_assert_false(verify_security_tag("appname/.."));
	g_assert_false(verify_security_tag("snap"));
	g_assert_false(verify_security_tag("snap."));
	g_assert_false(verify_security_tag("snap.name"));
	g_assert_false(verify_security_tag("snap.name."));
	g_assert_false(verify_security_tag("snap.name.app."));
	g_assert_false(verify_security_tag("snap.name.hook."));
	g_assert_false(verify_security_tag("snap!name.app"));
	g_assert_false(verify_security_tag("snap.-name.app"));
	g_assert_false(verify_security_tag("snap.name!app"));
	g_assert_false(verify_security_tag("snap.name.-app"));
	g_assert_false(verify_security_tag("snap.name.app!hook.foo"));
	g_assert_false(verify_security_tag("snap.name.app.hook!foo"));
	g_assert_false(verify_security_tag("snap.name.app.hook.-foo"));
	g_assert_false(verify_security_tag("snap.name.app.hook.f00"));
	g_assert_false(verify_security_tag("sna.pname.app"));
	g_assert_false(verify_security_tag("snap.n@me.app"));
	g_assert_false(verify_security_tag("SNAP.name.app"));
	g_assert_false(verify_security_tag("snap.Name.app"));
	g_assert_false(verify_security_tag("snap.0name.app"));
	g_assert_false(verify_security_tag("snap.-name.app"));
	g_assert_false(verify_security_tag("snap.name.@app"));
	g_assert_false(verify_security_tag(".name.app"));
	g_assert_false(verify_security_tag("snap..name.app"));
	g_assert_false(verify_security_tag("snap.name..app"));
	g_assert_false(verify_security_tag("snap.name.app.."));
}

static void test_sc_snap_name_validate()
{
	struct sc_error *err = NULL;

	// Smoke test, a valid snap name
	sc_snap_name_validate("hello-world", &err);
	g_assert_null(err);

	// Smoke test: invalid snap name (spaces are not allowed)
	sc_snap_name_validate("hello world", &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME));
	g_assert_cmpstr(sc_error_msg(err), ==,
			"invalid snap name \"hello world\"");
	sc_error_free(err);

	// Smoke test: empty name is not valid
	sc_snap_name_validate("", &err);
	g_assert_nonnull(err);
	g_assert_true(sc_error_match
		      (err, SC_SNAP_DOMAIN, SC_SNAP_INVALID_NAME));
	g_assert_cmpstr(sc_error_msg(err), ==, "invalid snap name \"\"");
	sc_error_free(err);
}

static void test_sc_snap_name_validate__respects_error_protocol()
{
	if (g_test_subprocess()) {
		sc_snap_name_validate("hello world", NULL);
		g_test_message("expected sc_snap_name_validate to return");
		g_test_fail();
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
	g_test_trap_assert_stderr("invalid snap name \"hello world\"\n");
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/snap/verify_security_tag", test_verify_security_tag);
	g_test_add_func("/snap/sc_snap_name_validate",
			test_sc_snap_name_validate);
	g_test_add_func("/snap/sc_snap_name_validate/respects_error_protocol",
			test_sc_snap_name_validate__respects_error_protocol);
}
