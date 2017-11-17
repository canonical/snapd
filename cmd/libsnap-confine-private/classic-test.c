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

#include "classic.h"
#include "classic.c"

#include <glib.h>

const char *os_release_classic = ""
    "NAME=\"Ubuntu\"\n"
    "VERSION=\"17.04 (Zesty Zapus)\"\n" "ID=ubuntu\n" "ID_LIKE=debian\n";

static void test_is_on_classic(void)
{
	g_file_set_contents("os-release.classic", os_release_classic,
			    strlen(os_release_classic), NULL);
	os_release = "os-release.classic";
	g_assert_true(is_running_on_classic_distribution());
	unlink("os-release.classic");
}

const char *os_release_core = ""
    "NAME=\"Ubuntu Core\"\n" "VERSION=\"16\"\n" "ID=ubuntu-core\n";

static void test_is_on_core(void)
{
	g_file_set_contents("os-release.core", os_release_core,
			    strlen(os_release_core), NULL);
	os_release = "os-release.core";
	g_assert_false(is_running_on_classic_distribution());
	unlink("os-release.core");
}

const char *os_release_classic_with_long_line = ""
    "NAME=\"Ubuntu\"\n"
    "VERSION=\"17.04 (Zesty Zapus)\"\n"
    "ID=ubuntu\n"
    "ID_LIKE=debian\n"
    "LONG=line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.line.";

static void test_is_on_classic_with_long_line(void)
{
	g_file_set_contents("os-release.classic-with-long-line",
			    os_release_classic, strlen(os_release_classic),
			    NULL);
	os_release = "os-release.classic-with-long-line";
	g_assert_true(is_running_on_classic_distribution());
	unlink("os-release.classic-with-long-line");
}

static void __attribute__ ((constructor)) init(void)
{
	g_test_add_func("/classic/on-classic", test_is_on_classic);
	g_test_add_func("/classic/on-classic-with-long-line",
			test_is_on_classic_with_long_line);
	g_test_add_func("/classic/on-core", test_is_on_core);
}
