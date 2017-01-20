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

static void test_sc_mount_cmd()
{
	char *cmd;

	// Typical mount
	cmd = sc_mount_cmd("/dev/sda3", "/mnt", "ext4", MS_RDONLY, NULL);
	g_assert_cmpstr(cmd, ==, "mount -t ext4 -o ro /dev/sda3 /mnt");
	free(cmd);

	// Bind mount
	cmd = sc_mount_cmd("/source", "/target", NULL, MS_BIND, NULL);
	g_assert_cmpstr(cmd, ==, "mount --bind /source /target");
	free(cmd);

	// + recursive

	cmd = sc_mount_cmd("/source", "/target", NULL, MS_BIND | MS_REC, NULL);
	g_assert_cmpstr(cmd, ==, "mount --rbind /source /target");
	free(cmd);

	// Shared subtree mount
	cmd = sc_mount_cmd("/place", "none", NULL, MS_SHARED, NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-shared /place");
	free(cmd);

	cmd = sc_mount_cmd("/place", "none", NULL, MS_SLAVE, NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-slave /place");
	free(cmd);

	cmd = sc_mount_cmd("/place", "none", NULL, MS_PRIVATE, NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-private /place");
	free(cmd);

	cmd = sc_mount_cmd("/place", "none", NULL, MS_UNBINDABLE, NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-unbindable /place");
	free(cmd);

	// + recursive

	cmd = sc_mount_cmd("/place", "none", NULL, MS_SHARED | MS_REC, NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-rshared /place");
	free(cmd);

	cmd = sc_mount_cmd("/place", "none", NULL, MS_SLAVE | MS_REC, NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-rslave /place");
	free(cmd);

	cmd = sc_mount_cmd("/place", "none", NULL, MS_PRIVATE | MS_REC, NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-rprivate /place");
	free(cmd);

	cmd = sc_mount_cmd("/place", "none", NULL, MS_UNBINDABLE | MS_REC,
			   NULL);
	g_assert_cmpstr(cmd, ==, "mount --make-runbindable /place");
	free(cmd);

	// Move
	cmd = sc_mount_cmd("/from", "/to", NULL, MS_MOVE, NULL);
	g_assert_cmpstr(cmd, ==, "mount --move /from /to");
	free(cmd);

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
	cmd = sc_mount_cmd(from, to, fstype, opts, NULL);
	const char *expected =
	    "mount -t fstype "
	    "--rbind --move --make-rshared --make-rslave --make-rprivate --make-runbindable "
	    "-o ro,nosuid,nodev,noexec,sync,remount,mand,dirsync,noatime,nodiratime,silent,"
	    "acl,relatime,kernmount,iversion,strictatime,lazytime "
	    "/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa "
	    "/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
	g_assert_cmpstr(cmd, ==, expected);
	free(cmd);
}

static void test_sc_umount_cmd()
{
	char *cmd;

	// Typical umount
	cmd = sc_umount_cmd("/mnt/foo", 0);
	g_assert_cmpstr(cmd, ==, "umount /mnt/foo");
	free(cmd);

	// Force
	cmd = sc_umount_cmd("/mnt/foo", MNT_FORCE);
	g_assert_cmpstr(cmd, ==, "umount --force /mnt/foo");
	free(cmd);

	// Detach
	cmd = sc_umount_cmd("/mnt/foo", MNT_DETACH);
	g_assert_cmpstr(cmd, ==, "umount --lazy /mnt/foo");
	free(cmd);

	// Expire
	cmd = sc_umount_cmd("/mnt/foo", MNT_EXPIRE);
	g_assert_cmpstr(cmd, ==, "umount --expire /mnt/foo");
	free(cmd);

	// O_NOFOLLOW variant for umount
	cmd = sc_umount_cmd("/mnt/foo", UMOUNT_NOFOLLOW);
	g_assert_cmpstr(cmd, ==, "umount --no-follow /mnt/foo");
	free(cmd);

	// Everything at once
	cmd =
	    sc_umount_cmd("/mnt/foo",
			  MNT_FORCE | MNT_DETACH | MNT_EXPIRE |
			  UMOUNT_NOFOLLOW);
	g_assert_cmpstr(cmd, ==,
			"umount --force --lazy --expire --no-follow /mnt/foo");
	free(cmd);
}

static void __attribute__ ((constructor)) init()
{
	g_test_add_func("/mount/sc_mount_opt2str", test_sc_mount_opt2str);
	g_test_add_func("/mount/sc_mount_cmd", test_sc_mount_cmd);
	g_test_add_func("/mount/sc_umount_cmd", test_sc_umount_cmd);
}
