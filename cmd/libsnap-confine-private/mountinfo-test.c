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

static void test_parse_mountinfo_entry__sysfs(void)
{
	const char *line =
	    "19 25 0:18 / /sys rw,nosuid,nodev,noexec,relatime shared:7 - sysfs sysfs rw";
	sc_mountinfo_entry *entry = sc_parse_mountinfo_entry(line);
	g_assert_nonnull(entry);
	g_test_queue_destroy((GDestroyNotify) sc_free_mountinfo_entry, entry);
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
static void test_parse_mountinfo_entry__snapd_ns(void)
{
	const char *line =
	    "104 23 0:19 /snapd/ns /run/snapd/ns rw,nosuid,noexec,relatime - tmpfs tmpfs rw,size=99840k,mode=755";
	sc_mountinfo_entry *entry = sc_parse_mountinfo_entry(line);
	g_assert_nonnull(entry);
	g_test_queue_destroy((GDestroyNotify) sc_free_mountinfo_entry, entry);
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

static void test_parse_mountinfo_entry__snapd_mnt(void)
{
	const char *line =
	    "256 104 0:3 mnt:[4026532509] /run/snapd/ns/hello-world.mnt rw - nsfs nsfs rw";
	sc_mountinfo_entry *entry = sc_parse_mountinfo_entry(line);
	g_assert_nonnull(entry);
	g_test_queue_destroy((GDestroyNotify) sc_free_mountinfo_entry, entry);
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

static void test_parse_mountinfo_entry__garbage(void)
{
	const char *line = "256 104 0:3";
	sc_mountinfo_entry *entry = sc_parse_mountinfo_entry(line);
	g_assert_null(entry);
}

static void test_parse_mountinfo_entry__no_tags(void)
{
	const char *line =
	    "1 2 3:4 root mount-dir mount-opts - fs-type mount-source super-opts";
	sc_mountinfo_entry *entry = sc_parse_mountinfo_entry(line);
	g_assert_nonnull(entry);
	g_test_queue_destroy((GDestroyNotify) sc_free_mountinfo_entry, entry);
	g_assert_cmpint(entry->mount_id, ==, 1);
	g_assert_cmpint(entry->parent_id, ==, 2);
	g_assert_cmpint(entry->dev_major, ==, 3);
	g_assert_cmpint(entry->dev_minor, ==, 4);
	g_assert_cmpstr(entry->root, ==, "root");
	g_assert_cmpstr(entry->mount_dir, ==, "mount-dir");
	g_assert_cmpstr(entry->mount_opts, ==, "mount-opts");
	g_assert_cmpstr(entry->optional_fields, ==, "");
	g_assert_cmpstr(entry->fs_type, ==, "fs-type");
	g_assert_cmpstr(entry->mount_source, ==, "mount-source");
	g_assert_cmpstr(entry->super_opts, ==, "super-opts");
	g_assert_null(entry->next);
}

static void test_parse_mountinfo_entry__one_tag(void)
{
	const char *line =
	    "1 2 3:4 root mount-dir mount-opts tag:1 - fs-type mount-source super-opts";
	sc_mountinfo_entry *entry = sc_parse_mountinfo_entry(line);
	g_assert_nonnull(entry);
	g_test_queue_destroy((GDestroyNotify) sc_free_mountinfo_entry, entry);
	g_assert_cmpint(entry->mount_id, ==, 1);
	g_assert_cmpint(entry->parent_id, ==, 2);
	g_assert_cmpint(entry->dev_major, ==, 3);
	g_assert_cmpint(entry->dev_minor, ==, 4);
	g_assert_cmpstr(entry->root, ==, "root");
	g_assert_cmpstr(entry->mount_dir, ==, "mount-dir");
	g_assert_cmpstr(entry->mount_opts, ==, "mount-opts");
	g_assert_cmpstr(entry->optional_fields, ==, "tag:1");
	g_assert_cmpstr(entry->fs_type, ==, "fs-type");
	g_assert_cmpstr(entry->mount_source, ==, "mount-source");
	g_assert_cmpstr(entry->super_opts, ==, "super-opts");
	g_assert_null(entry->next);
}

static void test_parse_mountinfo_entry__many_tags(void)
{
	const char *line =
	    "1 2 3:4 root mount-dir mount-opts tag:1 tag:2 tag:3 tag:4 - fs-type mount-source super-opts";
	sc_mountinfo_entry *entry = sc_parse_mountinfo_entry(line);
	g_assert_nonnull(entry);
	g_test_queue_destroy((GDestroyNotify) sc_free_mountinfo_entry, entry);
	g_assert_cmpint(entry->mount_id, ==, 1);
	g_assert_cmpint(entry->parent_id, ==, 2);
	g_assert_cmpint(entry->dev_major, ==, 3);
	g_assert_cmpint(entry->dev_minor, ==, 4);
	g_assert_cmpstr(entry->root, ==, "root");
	g_assert_cmpstr(entry->mount_dir, ==, "mount-dir");
	g_assert_cmpstr(entry->mount_opts, ==, "mount-opts");
	g_assert_cmpstr(entry->optional_fields, ==, "tag:1 tag:2 tag:3 tag:4");
	g_assert_cmpstr(entry->fs_type, ==, "fs-type");
	g_assert_cmpstr(entry->mount_source, ==, "mount-source");
	g_assert_cmpstr(entry->super_opts, ==, "super-opts");
	g_assert_null(entry->next);
}

static void test_parse_mountinfo_entry__empty_source(void)
{
	const char *line =
	    "304 301 0:45 / /snap/test-snapd-content-advanced-plug/x1 rw,relatime - tmpfs  rw";
	sc_mountinfo_entry *entry = sc_parse_mountinfo_entry(line);
	g_assert_nonnull(entry);
	g_test_queue_destroy((GDestroyNotify) sc_free_mountinfo_entry, entry);
	g_assert_cmpint(entry->mount_id, ==, 304);
	g_assert_cmpint(entry->parent_id, ==, 301);
	g_assert_cmpint(entry->dev_major, ==, 0);
	g_assert_cmpint(entry->dev_minor, ==, 45);
	g_assert_cmpstr(entry->root, ==, "/");
	g_assert_cmpstr(entry->mount_dir, ==,
			"/snap/test-snapd-content-advanced-plug/x1");
	g_assert_cmpstr(entry->mount_opts, ==, "rw,relatime");
	g_assert_cmpstr(entry->optional_fields, ==, "");
	g_assert_cmpstr(entry->fs_type, ==, "tmpfs");
	g_assert_cmpstr(entry->mount_source, ==, "");
	g_assert_cmpstr(entry->super_opts, ==, "rw");
	g_assert_null(entry->next);
}

static void test_parse_mountinfo_entry__octal_escaping(void)
{
	const char *line;
	struct sc_mountinfo_entry *entry;

	// The kernel escapes spaces as \040
	line = "2 1 0:54 / /tmp rw - tmpfs tricky\\040path rw";
	entry = sc_parse_mountinfo_entry(line);
	g_test_queue_destroy((GDestroyNotify) sc_free_mountinfo_entry, entry);
	g_assert_nonnull(entry);
	g_assert_cmpstr(entry->mount_source, ==, "tricky path");

	// kernel escapes newlines as \012
	line = "2 1 0:54 / /tmp rw - tmpfs tricky\\012path rw";
	entry = sc_parse_mountinfo_entry(line);
	g_test_queue_destroy((GDestroyNotify) sc_free_mountinfo_entry, entry);
	g_assert_nonnull(entry);
	g_assert_cmpstr(entry->mount_source, ==, "tricky\npath");

	// kernel escapes tabs as \011
	line = "2 1 0:54 / /tmp rw - tmpfs tricky\\011path rw";
	entry = sc_parse_mountinfo_entry(line);
	g_test_queue_destroy((GDestroyNotify) sc_free_mountinfo_entry, entry);
	g_assert_nonnull(entry);
	g_assert_cmpstr(entry->mount_source, ==, "tricky\tpath");

	// kernel escapes forward slashes as \057
	line = "2 1 0:54 / /tmp rw - tmpfs tricky\\057path rw";
	entry = sc_parse_mountinfo_entry(line);
	g_test_queue_destroy((GDestroyNotify) sc_free_mountinfo_entry, entry);
	g_assert_nonnull(entry);
	g_assert_cmpstr(entry->mount_source, ==, "tricky/path");
}

static void test_parse_mountinfo_entry__broken_octal_escaping(void)
{
	// Invalid octal escape sequences are left intact.
	const char *line =
	    "2074 27 0:54 / /tmp/strange-dir rw,relatime shared:1039 - tmpfs no\\888thing rw\\";
	struct sc_mountinfo_entry *entry = sc_parse_mountinfo_entry(line);
	g_assert_nonnull(entry);
	g_test_queue_destroy((GDestroyNotify) sc_free_mountinfo_entry, entry);
	g_assert_cmpint(entry->mount_id, ==, 2074);
	g_assert_cmpint(entry->parent_id, ==, 27);
	g_assert_cmpint(entry->dev_major, ==, 0);
	g_assert_cmpint(entry->dev_minor, ==, 54);
	g_assert_cmpstr(entry->root, ==, "/");
	g_assert_cmpstr(entry->mount_dir, ==, "/tmp/strange-dir");
	g_assert_cmpstr(entry->mount_opts, ==, "rw,relatime");
	g_assert_cmpstr(entry->optional_fields, ==, "shared:1039");
	g_assert_cmpstr(entry->fs_type, ==, "tmpfs");
	g_assert_cmpstr(entry->mount_source, ==, "no\\888thing");
	g_assert_cmpstr(entry->super_opts, ==, "rw\\");
	g_assert_null(entry->next);
}

static void test_parse_mountinfo_entry__unescaped_whitespace(void)
{
	// The kernel does not escape '\r'
	const char *line =
	    "2074 27 0:54 / /tmp/strange\rdir rw,relatime shared:1039 - tmpfs tmpfs rw";
	struct sc_mountinfo_entry *entry = sc_parse_mountinfo_entry(line);
	g_assert_nonnull(entry);
	g_test_queue_destroy((GDestroyNotify) sc_free_mountinfo_entry, entry);
	g_assert_cmpint(entry->mount_id, ==, 2074);
	g_assert_cmpint(entry->parent_id, ==, 27);
	g_assert_cmpint(entry->dev_major, ==, 0);
	g_assert_cmpint(entry->dev_minor, ==, 54);
	g_assert_cmpstr(entry->root, ==, "/");
	g_assert_cmpstr(entry->mount_dir, ==, "/tmp/strange\rdir");
	g_assert_cmpstr(entry->mount_opts, ==, "rw,relatime");
	g_assert_cmpstr(entry->optional_fields, ==, "shared:1039");
	g_assert_cmpstr(entry->fs_type, ==, "tmpfs");
	g_assert_cmpstr(entry->mount_source, ==, "tmpfs");
	g_assert_cmpstr(entry->super_opts, ==, "rw");
	g_assert_null(entry->next);
}

static void test_parse_mountinfo_entry__broken_9p_superblock(void)
{
	// Spaces in superblock options
	const char *line =
	    "1146 77 0:149 / /Docker/host rw,noatime - 9p drvfs rw,dirsync,aname=drvfs;path=C:\\Program Files\\Docker\\Docker\\resources;symlinkroot=/mnt/,mmap,access=client,msize=262144,trans=virtio";
	struct sc_mountinfo_entry *entry = sc_parse_mountinfo_entry(line);
	g_assert_nonnull(entry);
	g_test_queue_destroy((GDestroyNotify) sc_free_mountinfo_entry, entry);
	g_assert_cmpint(entry->mount_id, ==, 1146);
	g_assert_cmpint(entry->parent_id, ==, 77);
	g_assert_cmpint(entry->dev_major, ==, 0);
	g_assert_cmpint(entry->dev_minor, ==, 149);
	g_assert_cmpstr(entry->root, ==, "/");
	g_assert_cmpstr(entry->mount_dir, ==, "/Docker/host");
	g_assert_cmpstr(entry->mount_opts, ==, "rw,noatime");
	g_assert_cmpstr(entry->optional_fields, ==, "");
	g_assert_cmpstr(entry->fs_type, ==, "9p");
	g_assert_cmpstr(entry->mount_source, ==, "drvfs");
	g_assert_cmpstr(entry->super_opts, ==,
			"rw,dirsync,aname=drvfs;path=C:\\Program Files\\Docker\\Docker\\resources;symlinkroot=/mnt/,mmap,access=client,msize=262144,trans=virtio");
	g_assert_null(entry->next);
}

static void __attribute__((constructor)) init(void)
{
	g_test_add_func("/mountinfo/parse_mountinfo_entry/sysfs",
			test_parse_mountinfo_entry__sysfs);
	g_test_add_func("/mountinfo/parse_mountinfo_entry/snapd-ns",
			test_parse_mountinfo_entry__snapd_ns);
	g_test_add_func("/mountinfo/parse_mountinfo_entry/snapd-mnt",
			test_parse_mountinfo_entry__snapd_mnt);
	g_test_add_func("/mountinfo/parse_mountinfo_entry/garbage",
			test_parse_mountinfo_entry__garbage);
	g_test_add_func("/mountinfo/parse_mountinfo_entry/no_tags",
			test_parse_mountinfo_entry__no_tags);
	g_test_add_func("/mountinfo/parse_mountinfo_entry/one_tags",
			test_parse_mountinfo_entry__one_tag);
	g_test_add_func("/mountinfo/parse_mountinfo_entry/many_tags",
			test_parse_mountinfo_entry__many_tags);
	g_test_add_func
	    ("/mountinfo/parse_mountinfo_entry/empty_source",
	     test_parse_mountinfo_entry__empty_source);
	g_test_add_func("/mountinfo/parse_mountinfo_entry/octal_escaping",
			test_parse_mountinfo_entry__octal_escaping);
	g_test_add_func
	    ("/mountinfo/parse_mountinfo_entry/broken_octal_escaping",
	     test_parse_mountinfo_entry__broken_octal_escaping);
	g_test_add_func("/mountinfo/parse_mountinfo_entry/unescaped_whitespace",
			test_parse_mountinfo_entry__unescaped_whitespace);
	g_test_add_func("/mountinfo/parse_mountinfo_entry/broken_9p_superblock",
			test_parse_mountinfo_entry__broken_9p_superblock);
}
