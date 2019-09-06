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

#include "test-utils.h"
#include "string-utils.h"

#include "error.h"
#include "utils.h"

#include <glib.h>

void rm_rf_tmp(const char *dir)
{
	// Sanity check, don't remove anything that's not in the temporary
	// directory. This is here to prevent unintended data loss.
	if (!g_str_has_prefix(dir, "/tmp/"))
		die("refusing to remove: %s", dir);
	const gchar *working_directory = NULL;
	gchar **argv = NULL;
	gchar **envp = NULL;
	GSpawnFlags flags = G_SPAWN_SEARCH_PATH;
	GSpawnChildSetupFunc child_setup = NULL;
	gpointer user_data = NULL;
	gchar **standard_output = NULL;
	gchar **standard_error = NULL;
	gint exit_status = 0;
	GError *error = NULL;

	argv = calloc(5, sizeof *argv);
	if (argv == NULL)
		die("cannot allocate command argument array");
	argv[0] = g_strdup("rm");
	if (argv[0] == NULL)
		die("cannot allocate memory");
	argv[1] = g_strdup("-rf");
	if (argv[1] == NULL)
		die("cannot allocate memory");
	argv[2] = g_strdup("--");
	if (argv[2] == NULL)
		die("cannot allocate memory");
	argv[3] = g_strdup(dir);
	if (argv[3] == NULL)
		die("cannot allocate memory");
	argv[4] = NULL;
	g_assert_true(g_spawn_sync
		      (working_directory, argv, envp, flags, child_setup,
		       user_data, standard_output, standard_error, &exit_status,
		       &error));
	g_assert_true(g_spawn_check_exit_status(exit_status, NULL));
	if (error != NULL) {
		g_test_message("cannot remove temporary directory: %s\n",
			       error->message);
		g_error_free(error);
	}
	g_free(argv[0]);
	g_free(argv[1]);
	g_free(argv[2]);
	g_free(argv[3]);
	g_free(argv);
}

void
    __attribute__((sentinel)) test_argc_argv(int *argcp, char ***argvp, ...)
{
	int argc = 0;
	char **argv = NULL;
	g_test_queue_free(argv);

	va_list ap;
	va_start(ap, argvp);
	const char *arg;
	do {
		arg = va_arg(ap, const char *);
		// XXX: yeah, wrong way but the worse that can happen is for test to fail
		argv = realloc(argv, sizeof(const char **) * (argc + 1));
		g_assert_nonnull(argv);
		if (arg != NULL) {
			char *arg_copy = sc_strdup(arg);
			g_test_queue_free(arg_copy);
			argv[argc] = arg_copy;
			argc += 1;
		} else {
			argv[argc] = NULL;
		}
	} while (arg != NULL);
	va_end(ap);

	*argcp = argc;
	*argvp = argv;
}
