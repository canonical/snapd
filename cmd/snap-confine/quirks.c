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
#include "config.h"
#include "quirks.h"

#include <dirent.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>
#include <errno.h>

#include "../libsnap-confine-private/classic.h"
#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/mount-opt.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"
// XXX: for smaller patch, this should be in utils.h later
#include "user-support.h"

/**
 * Get the path to the mounted core snap in the execution environment.
 *
 * The core snap may be named just "core" (preferred) or "ubuntu-core"
 * (legacy).  The mount point does not depend on build-time configuration and
 * does not differ from distribution to distribution.
 **/
static const char *sc_get_inner_core_mount_point(void)
{
	const char *core_path = "/snap/core/current/";
	const char *ubuntu_core_path = "/snap/ubuntu-core/current/";
	static const char *result = NULL;
	if (result == NULL) {
		if (access(core_path, F_OK) == 0) {
			// Use the "core" snap if available.
			result = core_path;
		} else if (access(ubuntu_core_path, F_OK) == 0) {
			// If not try to fall back to the "ubuntu-core" snap.
			result = ubuntu_core_path;
		} else {
			die("cannot locate the core snap");
		}
	}
	return result;
}

/**
 * Mount a tmpfs at a given directory.
 *
 * The empty tmpfs is used as a substrate to create additional directories and
 * then bind mounts to other destinations.
 *
 * It is useful to poke unexpected holes in the read-only core snap.
 **/
static void sc_quirk_setup_tmpfs(const char *dirname)
{
	debug("mounting tmpfs at %s", dirname);
	if (mount("none", dirname, "tmpfs", MS_NODEV | MS_NOSUID, NULL) != 0) {
		die("cannot mount tmpfs at %s", dirname);
	};
}

/**
 * Create an empty directory and bind mount something there.
 *
 * The empty directory is created at destdir. The bind mount is
 * done from srcdir to destdir. The bind mount is performed with
 * caller-defined flags.
 **/
static void sc_quirk_mkdir_bind(const char *src_dir, const char *dest_dir,
				unsigned flags)
{
	flags |= MS_BIND;
	debug("creating empty directory at %s", dest_dir);
	if (sc_nonfatal_mkpath(dest_dir, 0755) < 0) {
		die("cannot create empty directory at %s", dest_dir);
	}
	char buf[1000] = { 0 };
	const char *flags_str = sc_mount_opt2str(buf, sizeof buf, flags);
	debug("performing operation: mount %s %s -o %s", src_dir, dest_dir,
	      flags_str);
	if (mount(src_dir, dest_dir, NULL, flags, NULL) != 0) {
		die("cannot perform operation: mount %s %s -o %s", src_dir,
		    dest_dir, flags_str);
	}
}

/**
 * Create a writable mimic directory based on reference directory.
 *
 * The mimic directory is a tmpfs populated with bind mounts to the (possibly
 * read only) directories in the reference directory. While all the read-only
 * content stays read-only the actual mimic directory is writable so additional
 * content can be placed there.
 *
 * Flags are forwarded to sc_quirk_mkdir_bind()
 **/
static void sc_quirk_create_writable_mimic(const char *mimic_dir,
					   const char *ref_dir, unsigned flags)
{
	debug("creating writable mimic directory %s based on %s", mimic_dir,
	      ref_dir);
	sc_quirk_setup_tmpfs(mimic_dir);

	// Now copy the ownership and permissions of the mimicked directory
	struct stat stat_buf;
	if (stat(ref_dir, &stat_buf) < 0) {
		die("cannot stat %s", ref_dir);
	}
	if (chown(mimic_dir, stat_buf.st_uid, stat_buf.st_gid) < 0) {
		die("cannot chown for %s", mimic_dir);
	}
	if (chmod(mimic_dir, stat_buf.st_mode) < 0) {
		die("cannot chmod for %s", mimic_dir);
	}

	debug("bind-mounting all the files from the reference directory");
	DIR *dirp SC_CLEANUP(sc_cleanup_closedir) = NULL;
	dirp = opendir(ref_dir);
	if (dirp == NULL) {
		die("cannot open reference directory %s", ref_dir);
	}
	struct dirent *entryp = NULL;
	do {
		char src_name[PATH_MAX * 2] = { 0 };
		char dest_name[PATH_MAX * 2] = { 0 };
		// Set errno to zero, if readdir fails it will not only return null but
		// set errno to a non-zero value. This is how we can differentiate
		// end-of-directory from an actual error.
		errno = 0;
		entryp = readdir(dirp);
		if (entryp == NULL && errno != 0) {
			die("cannot read another directory entry");
		}
		if (entryp == NULL) {
			break;
		}
		if (strcmp(entryp->d_name, ".") == 0
		    || strcmp(entryp->d_name, "..") == 0) {
			continue;
		}
		if (entryp->d_type != DT_DIR && entryp->d_type != DT_REG) {
			die("unsupported entry type of file %s (%d)",
			    entryp->d_name, entryp->d_type);
		}
		sc_must_snprintf(src_name, sizeof src_name, "%s/%s", ref_dir,
				 entryp->d_name);
		sc_must_snprintf(dest_name, sizeof dest_name, "%s/%s",
				 mimic_dir, entryp->d_name);
		sc_quirk_mkdir_bind(src_name, dest_name, flags);
	} while (entryp != NULL);
}

/**
 * Setup a quirk for LXD.
 *
 * An existing LXD snap relies on pre-chroot behavior to access /var/lib/lxd
 * while in devmode. Since that directory doesn't exist in the core snap the
 * quirk punches a custom hole so that this directory shows the hostfs content
 * if such directory exists on the host.
 *
 * See: https://bugs.launchpad.net/snap-confine/+bug/1613845
 **/
static void sc_setup_lxd_quirk(void)
{
	const char *hostfs_lxd_dir = SC_HOSTFS_DIR "/var/lib/lxd";
	if (access(hostfs_lxd_dir, F_OK) == 0) {
		const char *lxd_dir = "/var/lib/lxd";
		debug("setting up quirk for LXD (see LP: #1613845)");
		sc_quirk_mkdir_bind(hostfs_lxd_dir, lxd_dir,
				    MS_REC | MS_SLAVE | MS_NODEV | MS_NOSUID |
				    MS_NOEXEC);
	}
}

void sc_setup_quirks(void)
{
	// because /var/lib/snapd is essential let's move it to /tmp/snapd for a sec
	char snapd_tmp[] = "/tmp/snapd.quirks_XXXXXX";
	if (mkdtemp(snapd_tmp) == 0) {
		die("cannot create temporary directory for /var/lib/snapd mount point");
	}
	debug("performing operation: mount --move %s %s", "/var/lib/snapd",
	      snapd_tmp);
	if (mount("/var/lib/snapd", snapd_tmp, NULL, MS_MOVE, NULL)
	    != 0) {
		die("cannot perform operation: mount --move %s %s",
		    "/var/lib/snapd", snapd_tmp);
	}
	// now let's make /var/lib the vanilla /var/lib from the core snap
	char buf[PATH_MAX] = { 0 };
	sc_must_snprintf(buf, sizeof buf, "%s/var/lib",
			 sc_get_inner_core_mount_point());
	sc_quirk_create_writable_mimic("/var/lib", buf,
				       MS_RDONLY | MS_REC | MS_SLAVE | MS_NODEV
				       | MS_NOSUID);
	// now let's move /var/lib/snapd (that was originally there) back
	debug("performing operation: umount %s", "/var/lib/snapd");
	if (umount("/var/lib/snapd") != 0) {
		die("cannot perform operation: umount %s", "/var/lib/snapd");
	}
	debug("performing operation: mount --move %s %s", snapd_tmp,
	      "/var/lib/snapd");
	if (mount(snapd_tmp, "/var/lib/snapd", NULL, MS_MOVE, NULL)
	    != 0) {
		die("cannot perform operation: mount --move %s %s", snapd_tmp,
		    "/var/lib/snapd");
	}
	debug("performing operation: rmdir %s", snapd_tmp);
	if (rmdir(snapd_tmp) != 0) {
		die("cannot perform operation: rmdir %s", snapd_tmp);
	}
	// We are now ready to apply any quirks that relate to /var/lib
	sc_setup_lxd_quirk();
}
