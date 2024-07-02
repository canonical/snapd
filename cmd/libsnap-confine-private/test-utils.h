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

#ifndef SNAP_CONFINE_TEST_UTILS_H
#define SNAP_CONFINE_TEST_UTILS_H

#include <glib.h>

/**
 * Shell-out to "rm -rf -- $dir" as long as $dir is in /tmp.
 */
void rm_rf_tmp(const char *dir);

/**
 * Create an argc + argv pair out of a NULL terminated argument list.
 **/
void
    __attribute__((sentinel)) test_argc_argv(int *argcp, char ***argvp, ...);

typedef struct {
	int unused;
} snap_mount_dir_fixture;

void snap_mount_dir_fixture_setup(snap_mount_dir_fixture * fix,
				  gconstpointer user_data);
void snap_mount_dir_fixture_teardown(snap_mount_dir_fixture * fix,
				     gconstpointer user_data);

#endif
