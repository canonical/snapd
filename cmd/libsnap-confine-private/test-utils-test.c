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

#include "test-utils.h"

#include <fcntl.h>
#include <unistd.h>

#include <glib.h>

// Check that rm_rf_tmp doesn't remove things outside of /tmp
static void test_rm_rf_tmp()
{
	if (access("/nonexistent", F_OK) == 0) {
		g_test_message
		    ("/nonexistent exists but this test doesn't want it to");
		g_test_fail();
		return;
	}
	if (g_test_subprocess()) {
		rm_rf_tmp("/nonexistent");
		return;
	}
	g_test_trap_subprocess(NULL, 0, 0);
	g_test_trap_assert_failed();
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/test-utils/rm_rf_tmp", test_rm_rf_tmp);
}
