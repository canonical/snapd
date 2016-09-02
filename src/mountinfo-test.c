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

#include "mountinfo.h"
#include "mountinfo.c"

#include <glib.h>

static void test_parse_mountinfo_entry__sysfs()
{
	const char *line =
	    "19 25 0:18 / /sys rw,nosuid,nodev,noexec,relatime shared:7 - sysfs sysfs rw";
	struct mountinfo_entry *entry = parse_mountinfo_entry(line);
	g_assert_nonnull(entry);
	g_test_queue_destroy((GDestroyNotify) free_mountinfo_entry, entry);
	g_assert_cmpint(entry->mount_id, ==, 19);
	g_assert_cmpint(entry->parent_id, ==, 25);
	g_assert_cmpint(entry->dev_major, ==, 0);
	g_assert_cmpint(entry->dev_minor, ==, 18);
	g_assert_cmpstr(entry->root, ==, "/");
	g_assert_cmpstr(entry->mount_dir, ==, "/sys");
	g_assert_cmpstr(entry->mount_opts, ==,
			"rw,nosuid,nodev,noexec,relatime");
	g_assert_cmpstr(entry->optional_fields, ==, "shared:7");
	g_assert_cmpstr(entry->fs_type, ==, "sysfs");
	g_assert_cmpstr(entry->mount_source, ==, "sysfs");
	g_assert_cmpstr(entry->super_opts, ==, "rw");
	g_assert_null(entry->next);
}

// Parse the /run/snapd/ns bind mount (over itself)
// Note that /run is itself a tmpfs mount point.
static void test_parse_mountinfo_entry__snapd_ns()
{
	const char *line =
	    "104 23 0:19 /snapd/ns /run/snapd/ns rw,nosuid,noexec,relatime - tmpfs tmpfs rw,size=99840k,mode=755";
	struct mountinfo_entry *entry = parse_mountinfo_entry(line);
	g_assert_nonnull(entry);
	g_test_queue_destroy((GDestroyNotify) free_mountinfo_entry, entry);
	g_assert_cmpint(entry->mount_id, ==, 104);
	g_assert_cmpint(entry->parent_id, ==, 23);
	g_assert_cmpint(entry->dev_major, ==, 0);
	g_assert_cmpint(entry->dev_minor, ==, 19);
	g_assert_cmpstr(entry->root, ==, "/snapd/ns");
	g_assert_cmpstr(entry->mount_dir, ==, "/run/snapd/ns");
	g_assert_cmpstr(entry->mount_opts, ==, "rw,nosuid,noexec,relatime");
	g_assert_cmpstr(entry->optional_fields, ==, "");
	g_assert_cmpstr(entry->fs_type, ==, "tmpfs");
	g_assert_cmpstr(entry->mount_source, ==, "tmpfs");
	g_assert_cmpstr(entry->super_opts, ==, "rw,size=99840k,mode=755");
	g_assert_null(entry->next);
}

static void test_parse_mountinfo_entry__snapd_mnt()
{
	const char *line =
	    "256 104 0:3 mnt:[4026532509] /run/snapd/ns/hello-world.mnt rw - nsfs nsfs rw";
	struct mountinfo_entry *entry = parse_mountinfo_entry(line);
	g_assert_nonnull(entry);
	g_test_queue_destroy((GDestroyNotify) free_mountinfo_entry, entry);
	g_assert_cmpint(entry->mount_id, ==, 256);
	g_assert_cmpint(entry->parent_id, ==, 104);
	g_assert_cmpint(entry->dev_major, ==, 0);
	g_assert_cmpint(entry->dev_minor, ==, 3);
	g_assert_cmpstr(entry->root, ==, "mnt:[4026532509]");
	g_assert_cmpstr(entry->mount_dir, ==, "/run/snapd/ns/hello-world.mnt");
	g_assert_cmpstr(entry->mount_opts, ==, "rw");
	g_assert_cmpstr(entry->optional_fields, ==, "");
	g_assert_cmpstr(entry->fs_type, ==, "nsfs");
	g_assert_cmpstr(entry->mount_source, ==, "nsfs");
	g_assert_cmpstr(entry->super_opts, ==, "rw");
	g_assert_null(entry->next);
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/mountinfo/parse_mountinfo_entry/sysfs",
			test_parse_mountinfo_entry__sysfs);
	g_test_add_func("/mountinfo/parse_mountinfo_entry/snapd-ns",
			test_parse_mountinfo_entry__snapd_ns);
	g_test_add_func("/mountinfo/parse_mountinfo_entry/snapd-mnt",
			test_parse_mountinfo_entry__snapd_mnt);
}
