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
	g_assert_cmpint(sc_classify_distro(), ==, SC_DISTRO_CLASSIC);
	unlink("os-release.classic");
}

const char *os_release_core16 = ""
    "NAME=\"Ubuntu Core\"\n" "VERSION_ID=\"16\"\n" "ID=ubuntu-core\n";

static void test_is_on_core_on16(void)
{
	g_file_set_contents("os-release.core", os_release_core16,
			    strlen(os_release_core16), NULL);
	os_release = "os-release.core";
	g_assert_cmpint(sc_classify_distro(), ==, SC_DISTRO_CORE16);
	unlink("os-release.core");
}

const char *os_release_core18 = ""
    "NAME=\"Ubuntu Core\"\n" "VERSION_ID=\"18\"\n" "ID=ubuntu-core\n";

static void test_is_on_core_on18(void)
{
	g_file_set_contents("os-release.core", os_release_core18,
			    strlen(os_release_core18), NULL);
	os_release = "os-release.core";
	g_assert_cmpint(sc_classify_distro(), ==, SC_DISTRO_CORE_OTHER);
	unlink("os-release.core");
}

const char *os_release_core20 = ""
    "NAME=\"Ubuntu Core\"\n" "VERSION_ID=\"20\"\n" "ID=ubuntu-core\n";

static void test_is_on_core_on20(void)
{
	g_file_set_contents("os-release.core", os_release_core20,
			    strlen(os_release_core20), NULL);
	os_release = "os-release.core";
	g_assert_cmpint(sc_classify_distro(), ==, SC_DISTRO_CORE_OTHER);
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
	g_assert_cmpint(sc_classify_distro(), ==, SC_DISTRO_CLASSIC);
	unlink("os-release.classic-with-long-line");
}

static void test_should_use_normal_mode(void)
{
	g_assert_false(sc_should_use_normal_mode(SC_DISTRO_CORE16, "core"));
	g_assert_true(sc_should_use_normal_mode(SC_DISTRO_CORE_OTHER, "core"));
	g_assert_true(sc_should_use_normal_mode(SC_DISTRO_CLASSIC, "core"));

	g_assert_true(sc_should_use_normal_mode(SC_DISTRO_CORE16, "core18"));
	g_assert_true(sc_should_use_normal_mode
		      (SC_DISTRO_CORE_OTHER, "core18"));
	g_assert_true(sc_should_use_normal_mode(SC_DISTRO_CLASSIC, "core18"));
}

static void __attribute__ ((constructor)) init(void)
{
	g_test_add_func("/classic/on-classic", test_is_on_classic);
	g_test_add_func("/classic/on-classic-with-long-line",
			test_is_on_classic_with_long_line);
	g_test_add_func("/classic/on-core-on16", test_is_on_core_on16);
	g_test_add_func("/classic/on-core-on18", test_is_on_core_on18);
	g_test_add_func("/classic/on-core-on20", test_is_on_core_on20);
	g_test_add_func("/classic/should-use-normal-mode",
			test_should_use_normal_mode);
}
