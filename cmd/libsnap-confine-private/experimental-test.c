/*
 * Copyright (C) 2018 Canonical Ltd
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

#include "experimental.h"
#include "experimental.c"

#include <limits.h>

#include <glib.h>

#include "string-utils.h"
#include "test-utils.h"

static char *sc_testdir(void)
{
	char *d = g_dir_make_tmp(NULL, NULL);
	g_assert_nonnull(d);
	g_test_queue_free(d);
	g_test_queue_destroy((GDestroyNotify) rm_rf_tmp, d);
	return d;
}

// Set the feature flag directory to given value, useful for cleanup handlers.
static void set_feature_flag_dir(const char *dir)
{
	feature_flag_dir = dir;
}

// Mock the location of the feature flag directory.
static void sc_mock_feature_flag_dir(const char *d)
{
	g_test_queue_destroy((GDestroyNotify) set_feature_flag_dir,
			     (void *)feature_flag_dir);
	set_feature_flag_dir(d);
}

static void test_experimental_flag_active__missing_dir(void)
{
	const char *d = sc_testdir();
	char subd[PATH_MAX];
	sc_must_snprintf(subd, sizeof subd, "%s/absent", d);
	sc_mock_feature_flag_dir(subd);
	g_assert(!sc_experimental_flag_active(SC_PER_USER_MOUNT_NAMESPACE));
}

static void test_experimental_flag_active__missing_file(void)
{
	const char *d = sc_testdir();
	sc_mock_feature_flag_dir(d);
	g_assert(!sc_experimental_flag_active(SC_PER_USER_MOUNT_NAMESPACE));
}

static void test_experimental_flag_active__present_file(void)
{
	const char *d = sc_testdir();
	sc_mock_feature_flag_dir(d);
	char pname[PATH_MAX];
	sc_must_snprintf(pname, sizeof pname, "%s/per-user-mount-namespace", d);
	g_file_set_contents(pname, "", -1, NULL);

	g_assert(sc_experimental_flag_active(SC_PER_USER_MOUNT_NAMESPACE));
}

static void __attribute__ ((constructor)) init(void)
{
	g_test_add_func("/experimental/missing_dir",
			test_experimental_flag_active__missing_dir);
	g_test_add_func("/experimental/missing_file",
			test_experimental_flag_active__missing_file);
	g_test_add_func("/experimental/present_file",
			test_experimental_flag_active__present_file);
}
