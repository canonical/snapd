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

#include "privs.h"
#include "privs.c"

#include <glib.h>

// Test that dropping permissions really works
static void test_sc_privs_drop(void)
{
	if (geteuid() != 0 || getuid() == 0) {
		g_test_skip("run this test after chown root.root; chmod u+s");
		return;
	}
	if (getegid() != 0 || getgid() == 0) {
		g_test_skip("run this test after chown root.root; chmod g+s");
		return;
	}
	if (g_test_subprocess()) {
		// We start as a regular user with effective-root identity.
		g_assert_cmpint(getuid(), !=, 0);
		g_assert_cmpint(getgid(), !=, 0);

		g_assert_cmpint(geteuid(), ==, 0);
		g_assert_cmpint(getegid(), ==, 0);

		// We drop the privileges.
		sc_privs_drop();

		// The we are no longer root.
		g_assert_cmpint(getuid(), !=, 0);
		g_assert_cmpint(geteuid(), !=, 0);
		g_assert_cmpint(getgid(), !=, 0);
		g_assert_cmpint(getegid(), !=, 0);

		// We don't have any supplementary groups.
		gid_t groups[2];
		int num_groups = getgroups(1, groups);
		g_assert_cmpint(num_groups, ==, 1);
		g_assert_cmpint(groups[0], ==, getgid());

		// All done.
		return;
	}
	g_test_trap_subprocess(NULL, 0, G_TEST_SUBPROCESS_INHERIT_STDERR);
	g_test_trap_assert_passed();
}

static void __attribute__ ((constructor)) init(void)
{
	g_test_add_func("/privs/sc_privs_drop", test_sc_privs_drop);
}
