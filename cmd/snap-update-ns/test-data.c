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

#include "test-data.h"

#include <glib.h>

const char *test_entry_str_1 = "fsname-1 dir-1 type-1 opts-1 1 2";
const char *test_entry_str_2 = "fsname-2 dir-2 type-2 opts-2 3 4";

const struct sc_mount_entry test_entry_1 = {.entry = {
						      .mnt_fsname = "fsname-1",
						      .mnt_dir = "dir-1",
						      .mnt_type = "type-1",
						      .mnt_opts = "opts-1",
						      .mnt_freq = 1,
						      .mnt_passno = 2,
						      }
};

const struct sc_mount_entry test_entry_2 = {.entry = {
						      .mnt_fsname = "fsname-2",
						      .mnt_dir = "dir-2",
						      .mnt_type = "type-2",
						      .mnt_opts = "opts-2",
						      .mnt_freq = 3,
						      .mnt_passno = 4,
						      }
};

const struct mntent test_mnt_1 = {
	.mnt_fsname = "fsname-1",
	.mnt_dir = "dir-1",
	.mnt_type = "type-1",
	.mnt_opts = "opts-1",
	.mnt_freq = 1,
	.mnt_passno = 2,
};

const struct mntent test_mnt_2 = {
	.mnt_fsname = "fsname-2",
	.mnt_dir = "dir-2",
	.mnt_type = "type-2",
	.mnt_opts = "opts-2",
	.mnt_freq = 3,
	.mnt_passno = 4,
};

void test_looks_like_test_entry_1(const struct sc_mount_entry *entry)
{
	g_assert_cmpstr(entry->entry.mnt_fsname, ==, "fsname-1");
	g_assert_cmpstr(entry->entry.mnt_dir, ==, "dir-1");
	g_assert_cmpstr(entry->entry.mnt_type, ==, "type-1");
	g_assert_cmpstr(entry->entry.mnt_opts, ==, "opts-1");
	g_assert_cmpint(entry->entry.mnt_freq, ==, 1);
	g_assert_cmpint(entry->entry.mnt_passno, ==, 2);
}

void test_looks_like_test_entry_2(const struct sc_mount_entry *entry)
{
	g_assert_cmpstr(entry->entry.mnt_fsname, ==, "fsname-2");
	g_assert_cmpstr(entry->entry.mnt_dir, ==, "dir-2");
	g_assert_cmpstr(entry->entry.mnt_type, ==, "type-2");
	g_assert_cmpstr(entry->entry.mnt_opts, ==, "opts-2");
	g_assert_cmpint(entry->entry.mnt_freq, ==, 3);
	g_assert_cmpint(entry->entry.mnt_passno, ==, 4);
}
