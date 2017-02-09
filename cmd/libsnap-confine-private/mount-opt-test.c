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
	char buf[1000];
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, 0), ==, "");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_RDONLY), ==, "ro");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_NOSUID), ==,
			"nosuid");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_NODEV), ==,
			"nodev");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_NOEXEC), ==,
			"noexec");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_SYNCHRONOUS), ==,
			"sync");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_REMOUNT), ==,
			"remount");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_MANDLOCK), ==,
			"mand");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_DIRSYNC), ==,
			"dirsync");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_NOATIME), ==,
			"noatime");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_NODIRATIME), ==,
			"nodiratime");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_BIND), ==, "bind");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_REC | MS_BIND), ==,
			"rbind");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_MOVE), ==, "move");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_SILENT), ==,
			"silent");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_POSIXACL), ==,
			"acl");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_UNBINDABLE), ==,
			"unbindable");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_PRIVATE), ==,
			"private");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_REC | MS_PRIVATE),
			==, "rprivate");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_SLAVE), ==,
			"slave");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_REC | MS_SLAVE),
			==, "rslave");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_SHARED), ==,
			"shared");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_REC | MS_SHARED),
			==, "rshared");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_RELATIME), ==,
			"relatime");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_KERNMOUNT), ==,
			"kernmount");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_I_VERSION), ==,
			"iversion");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_STRICTATIME), ==,
			"strictatime");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_LAZYTIME), ==,
			"lazytime");
	// MS_NOSEC is not defined in userspace
	// MS_BORN is not defined in userspace
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_ACTIVE), ==,
			"active");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, MS_NOUSER), ==,
			"nouser");
	g_assert_cmpstr(sc_mount_opt2str(buf, sizeof buf, 0x300), ==, "0x300");
	// random compositions do work
	g_assert_cmpstr(sc_mount_opt2str
			(buf, sizeof buf, MS_RDONLY | MS_NOEXEC | MS_BIND), ==,
			"ro,noexec,bind");
}

static void test_sc_mount_cmd()
{
	char cmd[10000];

	// Typical mount
	sc_mount_cmd(cmd, sizeof cmd, "/dev/sda3", "/mnt", "ext4", MS_RDONLY,
		     NULL);
	g_assert_cmpstr(cmd, ==, "mount -t ext4 -o ro /dev/sda3 /mnt");

	// Bind mount
	sc_mount_cmd(cmd, sizeof cmd, "/source", "/target", NULL, MS_BIND,
		     NULL);
	g_assert_cmpstr(cmd, ==, "mount --bind /source /target");

	// + recursive
	sc_mount_cmd(cmd, sizeof cmd, "/source", "/target", NULL,
		     MS_BIND | MS_REC, NULL);
	g_assert_cmpstr(cmd, ==, "mount --rbind /source /target");

	// Shared subtree mount
	sc_mount_cmd(cmd, sizeof cmd, "/place", "none", NULL, MS_SHARED, NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-shared /place");

	sc_mount_cmd(cmd, sizeof cmd, "/place", "none", NULL, MS_SLAVE, NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-slave /place");

	sc_mount_cmd(cmd, sizeof cmd, "/place", "none", NULL, MS_PRIVATE, NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-private /place");

	sc_mount_cmd(cmd, sizeof cmd, "/place", "none", NULL, MS_UNBINDABLE,
		     NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-unbindable /place");

	// + recursive
	sc_mount_cmd(cmd, sizeof cmd, "/place", "none", NULL,
		     MS_SHARED | MS_REC, NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-rshared /place");

	sc_mount_cmd(cmd, sizeof cmd, "/place", "none", NULL, MS_SLAVE | MS_REC,
		     NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-rslave /place");

	sc_mount_cmd(cmd, sizeof cmd, "/place", "none", NULL,
		     MS_PRIVATE | MS_REC, NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-rprivate /place");

	sc_mount_cmd(cmd, sizeof cmd, "/place", "none", NULL,
		     MS_UNBINDABLE | MS_REC, NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-runbindable /place");

	// Move
	sc_mount_cmd(cmd, sizeof cmd, "/from", "/to", NULL, MS_MOVE, NULL);
	g_assert_cmpstr(cmd, ==, "mount --move /from /to");

	// Monster (invalid but let's format it)
	char from[PATH_MAX];
	char to[PATH_MAX];
	for (int i = 1; i < PATH_MAX - 1; ++i) {
		from[i] = 'a';
		to[i] = 'b';
	}
	from[0] = '/';
	to[0] = '/';
	from[PATH_MAX - 1] = 0;
	to[PATH_MAX - 1] = 0;
	int opts = MS_BIND | MS_MOVE | MS_SHARED | MS_SLAVE | MS_PRIVATE |
	    MS_UNBINDABLE | MS_REC | MS_RDONLY | MS_NOSUID | MS_NODEV |
	    MS_NOEXEC | MS_SYNCHRONOUS | MS_REMOUNT | MS_MANDLOCK | MS_DIRSYNC |
	    MS_NOATIME | MS_NODIRATIME | MS_BIND | MS_SILENT | MS_POSIXACL |
	    MS_RELATIME | MS_KERNMOUNT | MS_I_VERSION | MS_STRICTATIME |
	    MS_LAZYTIME;
	const char *fstype = "fstype";
	sc_mount_cmd(cmd, sizeof cmd, from, to, fstype, opts, NULL);
	const char *expected =
	    "mount -t fstype "
	    "--rbind --move --make-rshared --make-rslave --make-rprivate --make-runbindable "
	    "-o ro,nosuid,nodev,noexec,sync,remount,mand,dirsync,noatime,nodiratime,silent,"
	    "acl,relatime,kernmount,iversion,strictatime,lazytime "
	    "/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa "
	    "/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
	g_assert_cmpstr(cmd, ==, expected);
}

static void test_sc_umount_cmd()
{
	char cmd[1000];

	// Typical umount
	sc_umount_cmd(cmd, sizeof cmd, "/mnt/foo", 0);
	g_assert_cmpstr(cmd, ==, "umount /mnt/foo");

	// Force
	sc_umount_cmd(cmd, sizeof cmd, "/mnt/foo", MNT_FORCE);
	g_assert_cmpstr(cmd, ==, "umount --force /mnt/foo");

	// Detach
	sc_umount_cmd(cmd, sizeof cmd, "/mnt/foo", MNT_DETACH);
	g_assert_cmpstr(cmd, ==, "umount --lazy /mnt/foo");

	// Expire
	sc_umount_cmd(cmd, sizeof cmd, "/mnt/foo", MNT_EXPIRE);
	g_assert_cmpstr(cmd, ==, "umount --expire /mnt/foo");

	// O_NOFOLLOW variant for umount
	sc_umount_cmd(cmd, sizeof cmd, "/mnt/foo", UMOUNT_NOFOLLOW);
	g_assert_cmpstr(cmd, ==, "umount --no-follow /mnt/foo");

	// Everything at once
	sc_umount_cmd(cmd, sizeof cmd, "/mnt/foo",
		      MNT_FORCE | MNT_DETACH | MNT_EXPIRE | UMOUNT_NOFOLLOW);
	g_assert_cmpstr(cmd, ==,
			"umount --force --lazy --expire --no-follow /mnt/foo");
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/mount/sc_mount_opt2str", test_sc_mount_opt2str);
	g_test_add_func("/mount/sc_mount_cmd", test_sc_mount_cmd);
	g_test_add_func("/mount/sc_umount_cmd", test_sc_umount_cmd);
}
