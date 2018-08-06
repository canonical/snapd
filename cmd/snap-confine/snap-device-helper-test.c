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

#include "../libsnap-confine-private/test-utils.h"

#include <glib.h>
#include <glib/gstdio.h>
#include <fcntl.h>
#include <sys/types.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <string.h>

// TODO: build at runtime
static char *sdh_path = "snap-confine/snap-device-helper";

// A variant of unsetenv that is compatible with GDestroyNotify
static void my_unsetenv(const char *k)
{
	g_unsetenv(k);
}

// A variant of rm_rf_tmp that calls g_free() on its parameter
static void rm_rf_tmp_free(gchar * dirpath)
{
	rm_rf_tmp(dirpath);
	g_free(dirpath);
}

static int run_sdh(gchar * action,
		   gchar * appname, gchar * devpath, gchar * majmin)
{
	g_autofree gchar *mod_appname = g_strdup(appname);
	for (size_t i = 0; i < strlen(mod_appname); i++) {
		if (mod_appname[i] == '.') {
			mod_appname[i] = '_';
		}
	}
	g_debug("appname modified from %s to %s", appname, mod_appname);

	g_autoptr(GError) err = NULL;

	g_autoptr(GPtrArray) argv = g_ptr_array_new();
	g_ptr_array_add(argv, sdh_path);
	g_ptr_array_add(argv, action);
	g_ptr_array_add(argv, mod_appname);
	g_ptr_array_add(argv, devpath);
	g_ptr_array_add(argv, majmin);
	g_ptr_array_add(argv, NULL);

	int status = 0;

	gboolean ret = g_spawn_sync(NULL, (gchar **) argv->pdata, NULL, 0,
				    NULL, NULL, NULL, NULL, &status, &err);
	if (!ret) {
		g_debug("failed with: %s", err->message);
		return -2;
	}

	return WEXITSTATUS(status);
}

struct sdh_test_data {
	char *action;
	char *app;
	char *file_with_data;
	char *file_with_no_data;
};

static void test_sdh_action(gconstpointer test_data)
{
	struct sdh_test_data *td = (struct sdh_test_data *)test_data;

	gchar *mock_dir = g_dir_make_tmp(NULL, NULL);
	g_autofree gchar *app_dir = g_build_filename(mock_dir, td->app, NULL);
	g_autofree gchar *with_data = g_build_filename(mock_dir,
						       td->app,
						       td->file_with_data,
						       NULL);
	g_autofree gchar *without_data = g_build_filename(mock_dir,
							  td->app,
							  td->file_with_no_data,
							  NULL);
	int ret = 0;
	gchar *data = NULL;

	g_assert(g_mkdir_with_parents(app_dir, 0755) == 0);

	g_debug("mock cgroup dir: %s", mock_dir);

	g_setenv("DEVICES_CGROUP", mock_dir, TRUE);

	g_test_queue_destroy((GDestroyNotify) my_unsetenv, "DEVICES_CGROUP");
	g_test_queue_destroy((GDestroyNotify) rm_rf_tmp_free, mock_dir);

	ret =
	    run_sdh(td->action, td->app, "/devices/foo/block/sda/sda4", "8:4");
	g_assert_cmpint(ret, ==, 0);
	g_assert_true(g_file_get_contents(with_data, &data, NULL, NULL));
	g_assert_cmpstr(data, ==, "b 8:4 rwm\n");
	g_clear_pointer(&data, g_free);
	g_assert(g_remove(with_data) == 0);

	g_assert_false(g_file_get_contents(without_data, &data, NULL, NULL));

	ret = run_sdh(td->action, td->app, "/devices/foo/tty/ttyS0", "4:64");
	g_assert_cmpint(ret, ==, 0);
	g_assert_true(g_file_get_contents(with_data, &data, NULL, NULL));
	g_assert_cmpstr(data, ==, "c 4:64 rwm\n");
	g_clear_pointer(&data, g_free);
	g_assert(g_remove(with_data) == 0);

	g_assert_false(g_file_get_contents(without_data, &data, NULL, NULL));

}

static void test_sdh_err(void)
{
	int ret = 0;

	ret = run_sdh("add", "", "/devices/foo/block/sda/sda4", "8:4");
	g_assert_cmpint(ret, ==, 1);
	ret = run_sdh("add", "foo_bar", "", "8:4");
	g_assert_cmpint(ret, ==, 1);
	ret = run_sdh("add", "foo_bar", "/devices/foo/block/sda/sda4", "");
	g_assert_cmpint(ret, ==, 0);

	// mock some stuff so that we can reach the 'action' checks
	gchar *mock_dir = g_dir_make_tmp(NULL, NULL);
	g_autofree gchar *app_dir = g_build_filename(mock_dir, "foo.bar", NULL);
	g_assert(g_mkdir_with_parents(app_dir, 0755) == 0);
	g_setenv("DEVICES_CGROUP", mock_dir, TRUE);

	g_test_queue_destroy((GDestroyNotify) my_unsetenv, "DEVICES_CGROUP");
	g_test_queue_destroy((GDestroyNotify) rm_rf_tmp_free, mock_dir);

	ret =
	    run_sdh("badaction", "foo_bar", "/devices/foo/block/sda/sda4",
		    "8:4");
	g_assert_cmpint(ret, ==, 1);
}

static struct sdh_test_data add_data =
    { "add", "foo.bar", "devices.allow", "devices.deny" };
static struct sdh_test_data change_data =
    { "change", "foo.bar", "devices.allow", "devices.deny" };
static struct sdh_test_data remove_data =
    { "remove", "foo.bar", "devices.deny", "devices.allow" };

static void __attribute__ ((constructor)) init(void)
{

	g_test_add_data_func("/snap-device-helper/add",
			     &add_data, test_sdh_action);
	g_test_add_data_func("/snap-device-helper/change", &change_data,
			     test_sdh_action);
	g_test_add_data_func("/snap-device-helper/remove", &remove_data,
			     test_sdh_action);
	g_test_add_func("/snap-device-helper/err", test_sdh_err);
}
