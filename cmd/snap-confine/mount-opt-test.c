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

#include "mount-opt.h"
#include "mount-opt.c"

#include <sys/mount.h>
#include <glib.h>

static void test_sc_mount_opt2str()
{
	g_assert_cmpstr(sc_mount_opt2str(0), ==, "");
	g_assert_cmpstr(sc_mount_opt2str(MS_RDONLY), ==, "ro");
	g_assert_cmpstr(sc_mount_opt2str(MS_NOSUID), ==, "nosuid");
	g_assert_cmpstr(sc_mount_opt2str(MS_NODEV), ==, "nodev");
	g_assert_cmpstr(sc_mount_opt2str(MS_NOEXEC), ==, "noexec");
	g_assert_cmpstr(sc_mount_opt2str(MS_SYNCHRONOUS), ==, "sync");
	g_assert_cmpstr(sc_mount_opt2str(MS_REMOUNT), ==, "remount");
	g_assert_cmpstr(sc_mount_opt2str(MS_MANDLOCK), ==, "mand");
	g_assert_cmpstr(sc_mount_opt2str(MS_DIRSYNC), ==, "dirsync");
	g_assert_cmpstr(sc_mount_opt2str(MS_NOATIME), ==, "noatime");
	g_assert_cmpstr(sc_mount_opt2str(MS_NODIRATIME), ==, "nodiratime");
	g_assert_cmpstr(sc_mount_opt2str(MS_BIND), ==, "bind");
	g_assert_cmpstr(sc_mount_opt2str(MS_REC | MS_BIND), ==, "rbind");
	g_assert_cmpstr(sc_mount_opt2str(MS_MOVE), ==, "move");
	g_assert_cmpstr(sc_mount_opt2str(MS_SILENT), ==, "silent");
	g_assert_cmpstr(sc_mount_opt2str(MS_POSIXACL), ==, "acl");
	g_assert_cmpstr(sc_mount_opt2str(MS_UNBINDABLE), ==, "unbindable");
	g_assert_cmpstr(sc_mount_opt2str(MS_PRIVATE), ==, "private");
	g_assert_cmpstr(sc_mount_opt2str(MS_REC | MS_PRIVATE), ==, "rprivate");
	g_assert_cmpstr(sc_mount_opt2str(MS_SLAVE), ==, "slave");
	g_assert_cmpstr(sc_mount_opt2str(MS_REC | MS_SLAVE), ==, "rslave");
	g_assert_cmpstr(sc_mount_opt2str(MS_SHARED), ==, "shared");
	g_assert_cmpstr(sc_mount_opt2str(MS_REC | MS_SHARED), ==, "rshared");
	g_assert_cmpstr(sc_mount_opt2str(MS_RELATIME), ==, "relatime");
	g_assert_cmpstr(sc_mount_opt2str(MS_KERNMOUNT), ==, "kernmount");
	g_assert_cmpstr(sc_mount_opt2str(MS_I_VERSION), ==, "iversion");
	g_assert_cmpstr(sc_mount_opt2str(MS_STRICTATIME), ==, "strictatime");
	g_assert_cmpstr(sc_mount_opt2str(MS_LAZYTIME), ==, "lazytime");
	// MS_NOSEC is not defined in userspace
	// MS_BORN is not defined in userspace
	g_assert_cmpstr(sc_mount_opt2str(MS_ACTIVE), ==, "active");
	g_assert_cmpstr(sc_mount_opt2str(MS_NOUSER), ==, "nouser");
	g_assert_cmpstr(sc_mount_opt2str(0x300), ==, "0x300");
	// random compositions do work
	g_assert_cmpstr(sc_mount_opt2str(MS_RDONLY | MS_NOEXEC | MS_BIND), ==,
			"ro,noexec,bind");
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/mount/sc_mount_opt2str", test_sc_mount_opt2str);
}
