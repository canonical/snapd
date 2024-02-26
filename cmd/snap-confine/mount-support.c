/*
 * Copyright (C) 2015 Canonical Ltd
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

#ifdef HAVE_CONFIG_H
#include "config.h"
#endif

#include "mount-support.h"

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <libgen.h>
#include <limits.h>
#include <mntent.h>
#include <sched.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

#include "../libsnap-confine-private/apparmor-support.h"
#include "../libsnap-confine-private/classic.h"
#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/mount-opt.h"
#include "../libsnap-confine-private/mountinfo.h"
#include "../libsnap-confine-private/snap.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/tool.h"
#include "../libsnap-confine-private/utils.h"
#include "mount-support-nvidia.h"

#define MAX_BUF 1000
#define SNAP_PRIVATE_TMP_ROOT_DIR "/tmp/snap-private-tmp"

static void sc_detach_views_of_writable(sc_distro distro, bool normal_mode);

// TODO: simplify this, after all it is just a tmpfs
// TODO: fold this into bootstrap
static void setup_private_tmp(const char *snap_instance) {
    // Create a 0700 base directory. This is the "base" directory that is
    // protected from other users. This directory name is NOT randomly
    // generated. This has several properties:
    //
    // Users can relate to the name and can find the temporary directory as
    // visible from within the snap. If this directory was random it would be
    // harder to find because there may be situations in which multiple
    // directories related to the same snap name would exist.
    //
    // Snapd can partially manage the directory. Specifically on snap remove
    // snapd could remove the directory and everything in it, potentially
    // avoiding runaway disk use on a machine that either never reboots or uses
    // persistent /tmp directory.
    //
    // Underneath the base directory there is a "tmp" sub-directory that has
    // mode 1777 and behaves as a typical /tmp directory would. That directory
    // is used as a bind-mounted /tmp directory.
    //
    // Because the directories are reused across invocations by distinct users
    // and because the directories are trivially guessable, each invocation
    // unconditionally chowns/chmods them to appropriate values.
    char base[MAX_BUF] = {0};
    char tmp_dir[MAX_BUF] = {0};
    int private_tmp_root_fd SC_CLEANUP(sc_cleanup_close) = -1;
    int base_dir_fd SC_CLEANUP(sc_cleanup_close) = -1;
    int tmp_dir_fd SC_CLEANUP(sc_cleanup_close) = -1;

    /* Switch to root group so that mkdir and open calls below create
     * filesystem elements that are not owned by the user calling into
     * snap-confine. */
    sc_identity old = sc_set_effective_identity(sc_root_group_identity());

    // /tmp/snap-private-tmp should have already been created by
    // systemd-tmpfiles but we can try create it anyway since snapd may have
    // just been installed in which case the tmpfiles conf would not have
    // got executed yet
    if (mkdir(SNAP_PRIVATE_TMP_ROOT_DIR, 0700) < 0 && errno != EEXIST) {
        die("cannot create /tmp/snap-private-tmp");
    }
    private_tmp_root_fd = open(SNAP_PRIVATE_TMP_ROOT_DIR, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
    if (private_tmp_root_fd < 0) {
        die("cannot open %s", SNAP_PRIVATE_TMP_ROOT_DIR);
    }
    struct stat st;
    if (fstat(private_tmp_root_fd, &st) < 0) {
        die("cannot stat %s", SNAP_PRIVATE_TMP_ROOT_DIR);
    }
    if (st.st_uid != 0 || st.st_gid != 0 || st.st_mode != (S_IFDIR | 0700)) {
        die("%s has unexpected ownership / permissions", SNAP_PRIVATE_TMP_ROOT_DIR);
    }
    // Create /tmp/snap-private-tmp/snap.$SNAP_INSTANCE_NAME/ 0700 root.root.
    sc_must_snprintf(base, sizeof(base), "snap.%s", snap_instance);
    if (mkdirat(private_tmp_root_fd, base, 0700) < 0 && errno != EEXIST) {
        die("cannot create base directory: %s", base);
    }
    base_dir_fd = openat(private_tmp_root_fd, base, O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
    if (base_dir_fd < 0) {
        die("cannot open base directory: %s", base);
    }
    if (fstat(base_dir_fd, &st) < 0) {
        die("cannot stat %s/%s", SNAP_PRIVATE_TMP_ROOT_DIR, base);
    }
    if (st.st_uid != 0 || st.st_gid != 0 || st.st_mode != (S_IFDIR | 0700)) {
        die("%s/%s has unexpected ownership / permissions", SNAP_PRIVATE_TMP_ROOT_DIR, base);
    }
    // Create /tmp/$PRIVATE/snap.$SNAP_NAME/tmp 01777 root.root Ignore EEXIST
    // since we want to reuse and we will open with O_NOFOLLOW, below.
    if (mkdirat(base_dir_fd, "tmp", 01777) < 0 && errno != EEXIST) {
        die("cannot create private tmp directory %s/tmp", base);
    }
    (void)sc_set_effective_identity(old);
    tmp_dir_fd = openat(base_dir_fd, "tmp", O_RDONLY | O_DIRECTORY | O_CLOEXEC | O_NOFOLLOW);
    if (tmp_dir_fd < 0) {
        die("cannot open private tmp directory %s/tmp", base);
    }
    if (fstat(tmp_dir_fd, &st) < 0) {
        die("cannot stat %s/%s/tmp", SNAP_PRIVATE_TMP_ROOT_DIR, base);
    }
    if (st.st_uid != 0 || st.st_gid != 0 || st.st_mode != (S_IFDIR | 01777)) {
        die("%s/%s/tmp has unexpected ownership / permissions", SNAP_PRIVATE_TMP_ROOT_DIR, base);
    }
    // use the path to the file-descriptor in proc as the source mount point
    // as this is a symlink itself to the real directory at
    // /tmp/snap-private-tmp/snap.$SNAP_INSTANCE/tmp but doing it this way
    // helps avoid any potential race
    sc_must_snprintf(tmp_dir, sizeof(tmp_dir), "/proc/self/fd/%d", tmp_dir_fd);
    sc_do_mount(tmp_dir, "/tmp", NULL, MS_BIND, NULL);
    sc_do_mount("none", "/tmp", NULL, MS_PRIVATE, NULL);
}

// TODO: fold this into bootstrap
static void setup_private_pts(void) {
    // See https://www.kernel.org/doc/Documentation/filesystems/devpts.txt
    //
    // Ubuntu by default uses devpts 'single-instance' mode where
    // /dev/pts/ptmx is mounted with ptmxmode=0000. We don't want to change
    // the startup scripts though, so we follow the instructions in point
    // '4' of 'User-space changes' in the above doc. In other words, after
    // unshare(CLONE_NEWNS), we mount devpts with -o
    // newinstance,ptmxmode=0666 and then bind mount /dev/pts/ptmx onto
    // /dev/ptmx

    struct stat st;

    // Make sure /dev/pts/ptmx exists, otherwise the system doesn't provide the
    // isolation we require.
    if (stat("/dev/pts/ptmx", &st) != 0) {
        die("cannot stat /dev/pts/ptmx");
    }
    // Make sure /dev/ptmx exists so we can bind mount over it
    if (stat("/dev/ptmx", &st) != 0) {
        die("cannot stat /dev/ptmx");
    }
    // Since multi-instance, use ptmxmode=0666. The other options are
    // copied from /etc/default/devpts
    sc_do_mount("devpts", "/dev/pts", "devpts", MS_MGC_VAL, "newinstance,ptmxmode=0666,mode=0620,gid=5");
    sc_do_mount("/dev/pts/ptmx", "/dev/ptmx", "none", MS_BIND, NULL);
}

struct sc_mount {
    const char *path;
    bool is_bidirectional;
    // Alternate path defines the rbind mount "alternative" of path.
    // It exists so that we can make /media on systems that use /run/media.
    const char *altpath;
    // Optional mount points are not processed unless the source and
    // destination both exist.
    bool is_optional;
};

struct sc_mount_config {
    const char *rootfs_dir;
    // The struct is terminated with an entry with NULL path.
    const struct sc_mount *mounts;
    // Same as the structure above, but this is malloc-allocated.
    struct sc_mount *dynamic_mounts;
    sc_distro distro;
    bool normal_mode;
    const char *base_snap_name;
    const char *snap_instance;
};

/**
 * Ensures all required mount points have been created
 */
static void sc_create_mount_points(const char *scratch_dir, const struct sc_mount *mounts) {
    char dst[PATH_MAX] = {0};
    sc_identity old = sc_set_effective_identity(sc_root_group_identity());
    for (const struct sc_mount *mnt = mounts; mnt && mnt->path != NULL; mnt++) {
        sc_must_snprintf(dst, sizeof(dst), "%s/%s", scratch_dir, mnt->path);
        if (sc_nonfatal_mkpath(dst, 0755) < 0) {
            die("cannot create mount point %s", dst);
        }
    }
    (void)sc_set_effective_identity(old);
}

/**
 * Perform all the given bind mounts
 *
 * `mounts` is an array of sc_mount structures, each describing a bind mount
 * operation to be performed. An element carrying a `path` field set to NULL
 * marks the end of the list.
 *
 * Preconditions:
 *
 * - All the target directories must exist
 * - All the source directories must exist, unless the mount is bi-directional
 */
static void sc_do_mounts(const char *scratch_dir, const struct sc_mount *mounts) {
    char dst[PATH_MAX] = {0};
    // Bind mount certain directories from the host filesystem to the scratch
    // directory. By default mount events will propagate in both into and out
    // of the peer group. This way the running application can alter any global
    // state visible on the host and in other snaps. This can be restricted by
    // disabling the "is_bidirectional" flag as can be seen below.
    for (const struct sc_mount *mnt = mounts; mnt && mnt->path != NULL; mnt++) {
        if (mnt->is_bidirectional) {
            sc_identity old = sc_set_effective_identity(sc_root_group_identity());
            if (mkdir(mnt->path, 0755) < 0 && errno != EEXIST) {
                die("cannot create %s", mnt->path);
            }
            (void)sc_set_effective_identity(old);
        }
        sc_must_snprintf(dst, sizeof dst, "%s/%s", scratch_dir, mnt->path);
        if (mnt->is_optional) {
            bool ok = sc_do_optional_mount(mnt->path, dst, NULL, MS_REC | MS_BIND, NULL);
            if (!ok) {
                // If we cannot mount it, just continue.
                continue;
            }
        } else {
            sc_do_mount(mnt->path, dst, NULL, MS_REC | MS_BIND, NULL);
        }
        if (!mnt->is_bidirectional) {
            // Mount events will only propagate inwards to the namespace. This
            // way the running application cannot alter any global state apart
            // from that of its own snap.
            sc_do_mount("none", dst, NULL, MS_REC | MS_SLAVE, NULL);
        }
        if (mnt->altpath == NULL) {
            continue;
        }
        // An alternate path of mnt->path is provided at another location.
        // It should behave exactly the same as the original.
        sc_must_snprintf(dst, sizeof dst, "%s/%s", scratch_dir, mnt->altpath);
        struct stat stat_buf;
        if (lstat(dst, &stat_buf) < 0) {
            die("cannot lstat %s", dst);
        }
        if ((stat_buf.st_mode & S_IFMT) == S_IFLNK) {
            die("cannot bind mount alternate path over a symlink: %s", dst);
        }
        sc_do_mount(mnt->path, dst, NULL, MS_REC | MS_BIND, NULL);
        if (!mnt->is_bidirectional) {
            sc_do_mount("none", dst, NULL, MS_REC | MS_SLAVE, NULL);
        }
    }
}

/**
 * Create the /run/snapd/ns/snap.<snap-name>.fstab file.
 *
 * Initially, this will just contain the entry for the snap root filesystem (a
 * tmpfs), so that snap-update-ns will know about it and won't try to unmount
 * it.
 */
static void sc_initialize_ns_fstab(const char *snap_instance_name) {
    FILE *stream SC_CLEANUP(sc_cleanup_file) = NULL;
    char info_path[PATH_MAX] = {0};
    sc_must_snprintf(info_path, sizeof info_path, "/run/snapd/ns/snap.%s.fstab", snap_instance_name);
    int fd = -1;
    fd = open(info_path, O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC | O_NOFOLLOW, 0644);
    if (fd < 0) {
        die("cannot open %s", info_path);
    }
    if (fchown(fd, 0, 0) < 0) {
        die("cannot chown %s to root.root", info_path);
    }
    // The stream now owns the file descriptor.
    stream = fdopen(fd, "w");
    if (stream == NULL) {
        die("cannot get stream from file descriptor");
    }
    // We need to store an entry for the root directory, so that snap-update-ns
    // will know that it's a tmpfs created by us. It's not going to remount it,
    // so there's no need to be precise with the mount flags.
    fprintf(stream, "tmpfs / tmpfs x-snapd.origin=rootfs 0 0\n");
    if (ferror(stream) != 0) {
        die("I/O error when writing to %s", info_path);
    }
    if (fflush(stream) == EOF) {
        die("cannot flush %s", info_path);
    }
    debug("saved rootfs fstab entry to %s", info_path);
}

/**
 * Create root mountpoints and symbolic links.
 *
 * Enumerate the root entries in the filesystem provided by the provided
 * rootfs, and recreate all regular directories and symbolic links into the
 * scratch_dir.
 *
 * The root_mounts parameter lists the mounts that are going to be performed
 * later directly from the "/" directory of the system, so this function will
 * not touch them.
 */
static void sc_replicate_base_rootfs(const char *scratch_dir, const char *rootfs_dir,
                                     const struct sc_mount *root_mounts) {
    // First of all, fix the root filesystem:
    // - remove write permissions for group and others
    // - set the owner to root:root
    if (chmod(scratch_dir, 0755) < 0) {
        die("cannot change permissions on \"%s\"", scratch_dir);
    }
    if (chown(scratch_dir, 0, 0) < 0) {
        die("cannot change ownership on \"%s\"", scratch_dir);
    }

    int rootfs_fd = -1;
    // Note that the rootfs here is a path like /snap/<snap>/current, which is
    // always a symbolic link. Therefore, we cannot use O_NOFOLLOW here.
    rootfs_fd = open(rootfs_dir, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    if (rootfs_fd < 0) {
        die("cannot open directory \"%s\"", rootfs_dir);
    }

    // rootfs_fd is now managed by fdopendir() and should not be used after
    DIR *rootfs SC_CLEANUP(sc_cleanup_closedir) = fdopendir(rootfs_fd);
    if (rootfs == NULL) {
        die("cannot open directory \"%s\" from file descriptor", rootfs_dir);
    }

    // Will create folders/links as 0:0
    sc_identity old = sc_set_effective_identity(sc_root_group_identity());

    char full_path[PATH_MAX];
    // After we construct each entry's full path, we'll need to obtain the
    // entry's absolute path in the new rootfs, that is with the `scratch_dir`
    // prefix removed (the path_in_rootfs variable below). We'll do this by
    // computing the length of the `scratch_dir` prefix now and then using it
    // as the offset in `full_path` where the '/' of the confined silesystem is
    // located.
    const size_t scratch_dir_length = strlen(scratch_dir);

    while (true) {
        errno = 0;
        struct dirent *ent = readdir(rootfs);
        if (ent == NULL) break;

        if (sc_streq(ent->d_name, ".") || sc_streq(ent->d_name, "..")) {
            continue;
        }

        sc_must_snprintf(full_path, sizeof(full_path), "%s/%s", scratch_dir, ent->d_name);
        if (ent->d_type == DT_DIR) {
            if (mkdir(full_path, 0755) < 0) {
                die("cannot create directory \"%s\"", full_path);
            }

            // If the directory is listed in root_mounts skip it,
            // as it will be created and mounted in
            // sc_bootstrap_mount_namespace() later.
            bool skip_dir = false;
            const char *path_in_rootfs = full_path + scratch_dir_length;
            for (const struct sc_mount *mnt = root_mounts; mnt->path != NULL; mnt++) {
                if (sc_streq(path_in_rootfs, mnt->path) || sc_streq(path_in_rootfs, mnt->altpath)) {
                    skip_dir = true;
                    break;
                }
            }
            if (skip_dir) {
                continue;
            }
            // Also skip the /snap directory, as we'll mount it later
            if (sc_streq(path_in_rootfs, "/snap")) {
                continue;
            }

            char src_path[PATH_MAX];
            sc_must_snprintf(src_path, sizeof(src_path), "%s/%s", rootfs_dir, ent->d_name);
            sc_do_mount(src_path, full_path, NULL, MS_REC | MS_BIND, NULL);
        } else if (ent->d_type == DT_LNK) {
            char link_target[PATH_MAX + 1];
            ssize_t len = readlinkat(rootfs_fd, ent->d_name, link_target, sizeof(link_target) - 1);
            if (len < 0) {
                die("cannot read symbolic link \"%s/%s\"", rootfs_dir, ent->d_name);
            }
            // make sure the string is null terminated
            link_target[len] = '\0';

            // Both relative and absolute links will work out of the box, since
            // we are going to do a pivot_root to scratch_dir.
            if (symlink(link_target, full_path) < 0) {
                die("cannot create symbolic link \"%s\"", full_path);
            }
        } else if (ent->d_type == DT_REG) {
            // Create an empty file which can be used as a mount point
            int fd = open(full_path, O_CREAT | O_TRUNC, 0644);
            if (fd < 0) {
                die("cannot create mount point for file \"%s\"", full_path);
            }
            close(fd);
            char src_path[PATH_MAX];
            sc_must_snprintf(src_path, sizeof(src_path), "%s/%s", rootfs_dir, ent->d_name);
            sc_do_mount(src_path, full_path, NULL, MS_BIND, NULL);
        } else {
            die("unexpected directory entry \"%s\" of type %i encountered in "
                "\"%s\"",
                ent->d_name, ent->d_type, rootfs_dir);
        }
    }

    if (errno != 0) {
        die("cannot read directory entry in \"%s\"", rootfs_dir);
    }

    (void)sc_set_effective_identity(old);
}

/**
 * Bootstrap mount namespace.
 *
 * This is a chunk of tricky code that lets us have full control over the
 * layout and direction of propagation of mount events. The documentation below
 * assumes knowledge of the 'sharedsubtree.txt' document from the kernel source
 * tree.
 *
 * As a reminder two definitions are quoted below:
 *
 *  A 'propagation event' is defined as event generated on a vfsmount
 *  that leads to mount or unmount actions in other vfsmounts.
 *
 *  A 'peer group' is defined as a group of vfsmounts that propagate
 *  events to each other.
 *
 * (end of quote).
 *
 * The main idea is to setup a mount namespace that has a root filesystem with
 * vfsmounts and peer groups that, depending on the location, either isolate
 * or share with the rest of the system.
 *
 * The vast majority of the filesystem is shared in one direction. Events from
 * the outside (from the main mount namespace) propagate inside (to namespaces
 * of particular snaps) so things like new snap revisions, mounted drives, etc,
 * just show up as expected but even if a snap is exploited or malicious in
 * nature it cannot affect anything in another namespace where it might cause
 * security or stability issues.
 *
 * Selected directories (today just /media) can be shared in both directions.
 * This allows snaps with sufficient privileges to either create, through the
 * mount system call, additional mount points that are visible by the rest of
 * the system (both the main mount namespace and namespaces of individual
 * snaps) or remove them, through the unmount system call.
 **/
static void sc_bootstrap_mount_namespace(const struct sc_mount_config *config) {
    char scratch_dir[] = "/tmp/snap.rootfs_XXXXXX";
    char src[PATH_MAX] = {0};
    char dst[PATH_MAX] = {0};
    if (mkdtemp(scratch_dir) == NULL) {
        die("cannot create temporary directory for the root file system");
    }
    // NOTE: at this stage we just called unshare(CLONE_NEWNS). We are in a new
    // mount namespace and have a private list of mounts.
    debug("scratch directory for constructing namespace: %s", scratch_dir);
    // Make the root filesystem recursively shared. This way propagation events
    // will be shared with main mount namespace.
    sc_do_mount("none", "/", NULL, MS_REC | MS_SHARED, NULL);
    // Bind mount the temporary scratch directory for root filesystem over
    // itself so that it is a mount point. This is done so that it can become
    // unbindable as explained below.
    sc_do_mount(scratch_dir, scratch_dir, NULL, MS_BIND, NULL);
    // Make the scratch directory unbindable.
    //
    // This is necessary as otherwise a mount loop can occur and the kernel
    // would crash. The term unbindable simply states that it cannot be bind
    // mounted anywhere. When we construct recursive bind mounts below this
    // guarantees that this directory will not be replicated anywhere.
    sc_do_mount("none", scratch_dir, NULL, MS_UNBINDABLE, NULL);
    if (config->normal_mode) {
        sc_initialize_ns_fstab(config->snap_instance);
        // Create a tmpfs on scratch_dir; we'll them mount all the root
        // directories of the base snap onto it.
        sc_do_mount("none", scratch_dir, "tmpfs", 0, NULL);
        sc_replicate_base_rootfs(scratch_dir, config->rootfs_dir, config->mounts);
    } else {
        // Recursively bind mount desired root filesystem directory over the
        // scratch directory. This puts the initial content into the scratch
        // space and serves as a foundation for all subsequent operations
        // below.
        //
        // The mount is recursive because we need to accurately replicate the
        // state of the root filesystem into the scratch directory.
        sc_do_mount(config->rootfs_dir, scratch_dir, NULL, MS_REC | MS_BIND, NULL);
    }
    // Make the scratch directory recursively slave. Nothing done there will be
    // shared with the initial mount namespace. This effectively detaches us,
    // in one way, from the original namespace and coupled with pivot_root
    // below serves as the foundation of the mount sandbox.
    sc_do_mount("none", scratch_dir, NULL, MS_REC | MS_SLAVE, NULL);
    sc_do_mounts(scratch_dir, config->mounts);

    // Dynamic mounts handle things like user-specified home directories. These
    // can change between runs, so they are stored separately. As we don't know
    // these in advance, make sure paths also exist in the scratch dir.
    sc_create_mount_points(scratch_dir, config->dynamic_mounts);
    sc_do_mounts(scratch_dir, config->dynamic_mounts);

    if (config->normal_mode) {
        // Since we mounted /etc from the host filesystem to the scratch
        // directory, we may need to put certain directories from the desired
        // root filesystem (e.g. the core snap) back. This way the behavior of
        // running snaps is not affected by the alternatives directory from the
        // host, if one exists.
        //
        // Fixes the following bugs:
        //  - https://bugs.launchpad.net/snap-confine/+bug/1580018
        //  - https://bugzilla.opensuse.org/show_bug.cgi?id=1028568
        static const char *dirs_from_core[] = {"/etc/alternatives", "/etc/nsswitch.conf",
                                               // Some specific and privileged interfaces (e.g docker-support)
                                               // give access to apparmor_parser from the base snap which at a
                                               // minimum needs to use matching configuration from the base snap
                                               // instead of from the users host system.
                                               "/etc/apparmor", "/etc/apparmor.d",
                                               // Use ssl certs from the base by default unless
                                               // using Debian/Ubuntu classic (see below)
                                               "/etc/ssl", NULL};

        for (const char **dirs = dirs_from_core; *dirs != NULL; dirs++) {
            const char *dir = *dirs;

            // Special case for ubuntu/debian based
            // classic distros that use the core* snap:
            // here we use the host /etc/ssl
            // to support custom ca-cert setups
            if (sc_streq(dir, "/etc/ssl") && config->distro == SC_DISTRO_CLASSIC && sc_is_debian_like() &&
                sc_startswith(config->base_snap_name, "core")) {
                continue;
            }

            if (access(dir, F_OK) != 0) {
                continue;
            }
            struct stat dst_stat;
            struct stat src_stat;
            sc_must_snprintf(src, sizeof src, "%s%s", config->rootfs_dir, dir);
            sc_must_snprintf(dst, sizeof dst, "%s%s", scratch_dir, dir);
            if (lstat(src, &src_stat) != 0) {
                if (errno == ENOENT) {
                    continue;
                }
                die("cannot stat %s from desired rootfs", src);
            }
            if (!S_ISREG(src_stat.st_mode) && !S_ISDIR(src_stat.st_mode)) {
                debug(
                    "entry %s from the desired rootfs is not a file or "
                    "directory, skipping mount",
                    src);
                continue;
            }

            if (lstat(dst, &dst_stat) != 0) {
                if (errno == ENOENT) {
                    continue;
                }
                die("cannot stat %s from host", src);
            }
            if (!S_ISREG(dst_stat.st_mode) && !S_ISDIR(dst_stat.st_mode)) {
                debug(
                    "entry %s from the host is not a file or directory, "
                    "skipping mount",
                    src);
                continue;
            }

            if ((dst_stat.st_mode & S_IFMT) != (src_stat.st_mode & S_IFMT)) {
                debug("entries %s and %s are of different types, skipping mount", dst, src);
                continue;
            }
            // both source and destination exist where both are either files
            // or both are directories
            sc_do_mount(src, dst, NULL, MS_BIND, NULL);
            sc_do_mount("none", dst, NULL, MS_SLAVE, NULL);
        }
    }
    // The "core" base snap is special as it contains snapd and friends.
    // Other base snaps do not, so whenever a base snap other than core is
    // in use we need extra provisions for setting up internal tooling to
    // be available.
    //
    // However on a core18 (and similar) system the core snap is not
    // a special base anymore and we should map our own tooling in.
    if (config->distro == SC_DISTRO_CORE_OTHER || !sc_streq(config->base_snap_name, "core")) {
        // when bases are used we need to bind-mount the libexecdir
        // (that contains snap-exec) into /usr/lib/snapd of the
        // base snap so that snap-exec is available for the snaps
        // (base snaps do not ship snapd)

        // dst is always /usr/lib/snapd as this is where snapd
        // assumes to find snap-exec
        sc_must_snprintf(dst, sizeof dst, "%s/usr/lib/snapd", scratch_dir);

        // bind mount the current $ROOT/usr/lib/snapd path,
        // where $ROOT is either "/" or the "/snap/{core,snapd}/current"
        // that we are re-execing from
        char *src = NULL;
        char self[PATH_MAX + 1] = {0};
        ssize_t nread;
        nread = readlink("/proc/self/exe", self, sizeof self - 1);
        if (nread < 0) {
            die("cannot read /proc/self/exe");
        }
        // Though we initialized self to NULs and passed one less to
        // readlink, therefore guaranteeing that self is
        // zero-terminated, perform an explicit assignment to make
        // Coverity happy.
        self[nread] = '\0';
        // this cannot happen except when the kernel is buggy
        if (strstr(self, "/snap-confine") == NULL) {
            die("cannot use result from readlink: %s", self);
        }
        src = dirname(self);
        // dirname(path) might return '.' depending on path.
        // /proc/self/exe should always point
        // to an absolute path, but let's guarantee that.
        if (src[0] != '/') {
            die("cannot use the result of dirname(): %s", src);
        }

        sc_do_mount(src, dst, NULL, MS_BIND | MS_RDONLY, NULL);
        sc_do_mount("none", dst, NULL, MS_SLAVE, NULL);
    }
    // Bind mount the directory where all snaps are mounted. The location of
    // the this directory on the host filesystem may not match the location in
    // the desired root filesystem. In the "core" and "ubuntu-core" snaps the
    // directory is always /snap. On the host it is a build-time configuration
    // option stored in SNAP_MOUNT_DIR. In legacy mode (or in other words, not
    // in normal mode), we don't need to do this because /snap is fixed and
    // already contains the correct view of the mounted snaps.
    if (config->normal_mode) {
        sc_must_snprintf(dst, sizeof dst, "%s/snap", scratch_dir);
        sc_do_mount(SNAP_MOUNT_DIR, dst, NULL, MS_BIND | MS_REC, NULL);
        sc_do_mount("none", dst, NULL, MS_REC | MS_SLAVE, NULL);
    }
    // Ensure that hostfs exists and is group-owned by root. We may have (now
    // or earlier) created the directory as the user who first ran a snap on a
    // given system and the group identity of that user is visible on disk.
    // This was LP:#1665004
    struct stat sb;
    if (stat(SC_HOSTFS_DIR, &sb) < 0) {
        if (errno == ENOENT) {
            // Create the hostfs directory if one is missing. This directory is
            // a part of packaging now so perhaps this code can be removed
            // later. Note: we use 0000 as permissions here, to avoid the risk
            // that the user manages to fiddle with the newly created directory
            // before we have the chance to chown it to root:root. We are
            // setting the usual 0755 permissions just after the chown below.
            if (mkdir(SC_HOSTFS_DIR, 0000) < 0) {
                die("cannot perform operation: mkdir %s", SC_HOSTFS_DIR);
            }
            if (chown(SC_HOSTFS_DIR, 0, 0) < 0) {
                die("cannot set root ownership on %s directory", SC_HOSTFS_DIR);
            }
            if (chmod(SC_HOSTFS_DIR, 0755) < 0) {
                die("cannot set 0755 permissions on %s directory", SC_HOSTFS_DIR);
            }
        } else {
            die("cannot stat %s", SC_HOSTFS_DIR);
        }
    } else if (sb.st_uid != 0 || sb.st_gid != 0) {
        die("%s is not owned by root", SC_HOSTFS_DIR);
    }
    // Make the upcoming "put_old" directory for pivot_root private so that
    // mount events don't propagate to any peer group. In practice pivot root
    // has a number of undocumented requirements and one of them is that the
    // "put_old" directory (the second argument) cannot be shared in any way.
    sc_must_snprintf(dst, sizeof dst, "%s/%s", scratch_dir, SC_HOSTFS_DIR);
    sc_do_mount(dst, dst, NULL, MS_BIND, NULL);
    sc_do_mount("none", dst, NULL, MS_PRIVATE, NULL);
    // On classic mount the nvidia driver. Ideally this would be done in an
    // uniform way after pivot_root but this is good enough and requires less
    // code changes the nvidia code assumes it has access to the existing
    // pre-pivot filesystem.
    if (config->distro == SC_DISTRO_CLASSIC) {
        sc_mount_nvidia_driver(scratch_dir, config->base_snap_name);
    }
    // XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
    //                    pivot_root
    // XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
    // Use pivot_root to "chroot" into the scratch directory.
    //
    // Q: Why are we using something as esoteric as pivot_root(2)?
    // A: Because this makes apparmor handling easy. Using a normal chroot
    // makes all apparmor rules conditional.  We are either running on an
    // all-snap system where this would-be chroot didn't happen and all the
    // rules see / as the root file system _OR_ we are running on top of a
    // classic distribution and this chroot has now moved all paths to
    // /tmp/snap.rootfs_*.
    //
    // Because we are using unshare(2) with CLONE_NEWNS we can essentially use
    // pivot_root just like chroot but this makes apparmor unaware of the old
    // root so everything works okay.
    //
    // HINT: If you are debugging this and are trying to see why pivot_root
    // happens to return EINVAL with any changes you may be making, please
    // consider applying
    // misc/0001-Add-printk-based-debugging-to-pivot_root.patch to your tree
    // kernel.
    debug("performing operation: pivot_root %s %s", scratch_dir, dst);
    if (syscall(SYS_pivot_root, scratch_dir, dst) < 0) {
        die("cannot perform operation: pivot_root %s %s", scratch_dir, dst);
    }
    // Unmount the self-bind mount over the scratch directory created earlier
    // in the original root filesystem (which is now mounted on SC_HOSTFS_DIR).
    // This way we can remove the temporary directory we created and "clean up"
    // after ourselves nicely.
    sc_must_snprintf(dst, sizeof dst, "%s/%s", SC_HOSTFS_DIR, scratch_dir);
    sc_do_umount(dst, UMOUNT_NOFOLLOW);
    // Remove the scratch directory. Note that we are using the path that is
    // based on the old root filesystem as after pivot_root we cannot guarantee
    // what is present at the same location normally. (It is probably an empty
    // /tmp directory that is populated in another place).
    debug("performing operation: rmdir %s", dst);
    if (rmdir(scratch_dir) < 0) {
        die("cannot perform operation: rmdir %s", dst);
    };
    // Make the old root filesystem recursively slave. This way operations
    // performed in this mount namespace will not propagate to the peer group.
    // This is another essential part of the confinement system.
    sc_do_mount("none", SC_HOSTFS_DIR, NULL, MS_REC | MS_SLAVE, NULL);
    // Detach the redundant hostfs version of sysfs since it shows up in the
    // mount table and software inspecting the mount table may become confused
    // (eg, docker and LP:# 162601).
    sc_must_snprintf(src, sizeof src, "%s/sys", SC_HOSTFS_DIR);
    sc_do_umount(src, UMOUNT_NOFOLLOW | MNT_DETACH);
    // Detach the redundant hostfs version of /dev since it shows up in the
    // mount table and software inspecting the mount table may become confused.
    sc_must_snprintf(src, sizeof src, "%s/dev", SC_HOSTFS_DIR);
    sc_do_umount(src, UMOUNT_NOFOLLOW | MNT_DETACH);
    // Detach the redundant hostfs version of /proc since it shows up in the
    // mount table and software inspecting the mount table may become confused.
    sc_must_snprintf(src, sizeof src, "%s/proc", SC_HOSTFS_DIR);
    sc_do_umount(src, UMOUNT_NOFOLLOW | MNT_DETACH);
    // Detach both views of /writable: the one from hostfs and the one directly
    // visible in /writable. Interfaces don't grant access to this directory
    // and it has a large duplicated view of many mount points.  Note that this
    // is only applicable to ubuntu-core systems.
    sc_detach_views_of_writable(config->distro, config->normal_mode);
}

static void sc_detach_views_of_writable(sc_distro distro, bool normal_mode) {
    // Note that prior to detaching either mount point we switch the
    // propagation to private to both limit the change to just this view and to
    // prevent otherwise occurring event propagation from self-conflicting and
    // returning EBUSY. A similar approach is used by snap-update-ns and is
    // documented in umount(2).
    const char *writable_dir = "/writable";
    const char *hostfs_writable_dir = "/var/lib/snapd/hostfs/writable";

    // Writable only exists on ubuntu-core.
    if (distro == SC_DISTRO_CLASSIC) {
        return;
    }
    // On all core distributions we see /var/lib/snapd/hostfs/writable that
    // exposes writable, with a structure specific to ubuntu-core.
    debug("detaching %s", hostfs_writable_dir);
    sc_do_mount("none", hostfs_writable_dir, NULL, MS_REC | MS_PRIVATE, NULL);
    sc_do_umount(hostfs_writable_dir, UMOUNT_NOFOLLOW | MNT_DETACH);

    // On ubuntu-core 16, when the executed snap uses core as base we also see
    // the /writable that we directly inherited from the initial mount
    // namespace.
    if (distro == SC_DISTRO_CORE16 && !normal_mode) {
        debug("detaching %s", writable_dir);
        sc_do_mount("none", writable_dir, NULL, MS_REC | MS_PRIVATE, NULL);
        sc_do_umount(writable_dir, UMOUNT_NOFOLLOW | MNT_DETACH);
    }
}

/**
 * @path:    a pathname where / replaced with '\0'.
 * @offsetp: pointer to int showing which path segment was last seen.
 *           Updated on return to reflect the next segment.
 * @fulllen: full original path length.
 * Returns a pointer to the next path segment, or NULL if done.
 */
static char *__attribute__((used)) get_nextpath(char *path, size_t *offsetp, size_t fulllen) {
    size_t offset = *offsetp;

    if (offset >= fulllen) return NULL;

    while (offset < fulllen && path[offset] != '\0') offset++;
    while (offset < fulllen && path[offset] == '\0') offset++;

    *offsetp = offset;
    return (offset < fulllen) ? &path[offset] : NULL;
}

/**
 * Check that @subdir is a subdir of @dir.
 **/
static bool __attribute__((used)) is_subdir(const char *subdir, const char *dir) {
    size_t dirlen = strlen(dir);
    size_t subdirlen = strlen(subdir);

    // @dir has to be at least as long as @subdir
    if (subdirlen < dirlen) return false;
    // @dir has to be a prefix of @subdir
    if (strncmp(subdir, dir, dirlen) != 0) return false;
    // @dir can look like "path/" (that is, end with the directory separator).
    // When that is the case then given the test above we can be sure @subdir
    // is a real subdirectory.
    if (dirlen > 0 && dir[dirlen - 1] == '/') return true;
    // @subdir can look like "path/stuff" and when the directory separator
    // is exactly at the spot where @dir ends (that is, it was not caught
    // by the test above) then @subdir is a real subdirectory.
    if (subdir[dirlen] == '/' && dirlen > 0) return true;
    // If both @dir and @subdir have identical length then given that the
    // prefix check above @subdir is a real subdirectory.
    if (subdirlen == dirlen) return true;
    return false;
}

static struct sc_mount *sc_homedir_mounts(const struct sc_invocation *inv) {
    if (inv->num_homedirs == 0) {
        return NULL;
    }

    // We add one element for the end-of-array indicator.
    struct sc_mount *mounts = calloc(inv->num_homedirs + 1, sizeof(struct sc_mount));
    if (mounts == NULL) {
        die("cannot allocate mount data for homedirs");
    }

    // Copy inv->homedirs to the mount structures
    for (int i = 0; i < inv->num_homedirs; i++) {
        debug("Adding homedir: %s", inv->homedirs[i]);
        mounts[i].path = sc_strdup(inv->homedirs[i]);
        mounts[i].is_bidirectional = true;
    }
    return mounts;
}

static void sc_free_dynamic_mounts(struct sc_mount *mounts) {
    // This is in line with normal free semantics.
    if (mounts == NULL) {
        return;
    }

    // Cleanup allocated resources by each of the mount
    // structures. The array will be terminated by a single zeroed
    // entry.
    for (int i = 0; mounts[i].path != NULL; i++) {
        free((void *)mounts[i].path);
    }
    free(mounts);
}

void sc_populate_mount_ns(struct sc_apparmor *apparmor, int snap_update_ns_fd, const sc_invocation *inv,
                          const gid_t real_gid, const gid_t saved_gid) {
    // Classify the current distribution, as claimed by /etc/os-release.
    sc_distro distro = sc_classify_distro();

    // Check which mode we should run in, normal or legacy.
    if (inv->is_normal_mode) {
        // In normal mode we use the base snap as / and set up several bind
        // mounts.
        static const struct sc_mount mounts[] = {
            {.path = "/dev"},                                // because it contains devices on host OS
            {.path = "/etc"},                                // because that's where /etc/resolv.conf lives,
                                                             // perhaps a bad idea
            {.path = "/home"},                               // to support /home/*/snap and home interface
            {.path = "/root"},                               // because that is $HOME for services
            {.path = "/proc"},                               // fundamental filesystem
            {.path = "/sys"},                                // fundamental filesystem
            {.path = "/tmp"},                                // to get writable tmp
            {.path = "/var/snap"},                           // to get access to global snap data
            {.path = "/var/lib/snapd"},                      // to get access to snapd state and
                                                             // seccomp profiles
            {.path = "/var/tmp"},                            // to get access to the other temporary directory
            {.path = "/run"},                                // to get /run with sockets and what not
            {.path = "/lib/modules", .is_optional = true},   // access to the modules of the running kernel
            {.path = "/lib/firmware", .is_optional = true},  // access to the firmware of the running kernel
            {.path = "/usr/src"},                            // FIXME: move to SecurityMounts in
                                                             // system-trace interface
            {.path = "/var/log"},                            // FIXME: move to SecurityMounts in
                                                             // log-observe interface
#ifdef MERGED_USR
            {.path = "/run/media",
             .is_bidirectional = true,
             .altpath = "/media"},  // access to the users removable devices
#else
            {.path = "/media", .is_bidirectional = true},  // access to the users removable devices
#endif                                                         // MERGED_USR
            {.path = "/run/netns", .is_bidirectional = true},  // access to the 'ip netns' network namespaces
            // The /mnt directory is optional in base snaps to ensure backwards
            // compatibility with the first version of base snaps that was
            // released.
            {.path = "/mnt", .is_optional = true},                 // to support the removable-media interface
            {.path = "/var/lib/extrausers", .is_optional = true},  // access to UID/GID of extrausers (if available)
            {},
        };
        struct sc_mount_config normal_config = {
            .rootfs_dir = inv->rootfs_dir,
            .mounts = mounts,
            // Homedir mounts are user-specified paths that snaps are allowed
            // to access, which don't reside in the regular home path. They can
            // change between runs, so we must dynamically handle them.
            .dynamic_mounts = sc_homedir_mounts(inv),
            .distro = distro,
            .normal_mode = true,
            .base_snap_name = inv->base_snap_name,
            .snap_instance = inv->snap_instance,
        };
        sc_bootstrap_mount_namespace(&normal_config);
        sc_free_dynamic_mounts(normal_config.dynamic_mounts);
        normal_config.dynamic_mounts = NULL;
    } else {
        // In legacy mode we don't pivot to a base snap's rootfs and instead
        // just arrange bi-directional mount propagation for two directories.
        static const struct sc_mount mounts[] = {
            {.path = "/media", .is_bidirectional = true},
            {.path = "/run/netns", .is_bidirectional = true},
            {},
        };
        struct sc_mount_config legacy_config = {
            .rootfs_dir = "/",
            .mounts = mounts,
            // XXX: should we support Homedir mount in legacy mode?
            .distro = distro,
            .normal_mode = false,
            .base_snap_name = inv->base_snap_name,
        };
        sc_bootstrap_mount_namespace(&legacy_config);
    }

    // TODO: rename this and fold it into bootstrap
    setup_private_tmp(inv->snap_instance);
    // set up private /dev/pts
    // TODO: fold this into bootstrap
    setup_private_pts();

    // setup the security backend bind mounts
    sc_call_snap_update_ns(snap_update_ns_fd, inv->snap_instance, apparmor);
}

static bool is_mounted_with_shared_option(const char *dir) __attribute__((nonnull(1)));

static bool is_mounted_with_shared_option(const char *dir) {
    sc_mountinfo *sm SC_CLEANUP(sc_cleanup_mountinfo) = NULL;
    sm = sc_parse_mountinfo(NULL);
    if (sm == NULL) {
        die("cannot parse /proc/self/mountinfo");
    }
    sc_mountinfo_entry *entry = sc_first_mountinfo_entry(sm);
    while (entry != NULL) {
        const char *mount_dir = entry->mount_dir;
        if (sc_streq(mount_dir, dir)) {
            const char *optional_fields = entry->optional_fields;
            if (strstr(optional_fields, "shared:") != NULL) {
                return true;
            }
        }
        entry = sc_next_mountinfo_entry(entry);
    }
    return false;
}

void sc_ensure_shared_snap_mount(void) {
    if (!is_mounted_with_shared_option("/") && !is_mounted_with_shared_option(SNAP_MOUNT_DIR)) {
        // TODO: We could be more aggressive and refuse to function but since
        // we have no data on actual environments that happen to limp along in
        // this configuration let's not do that yet.  This code should be
        // removed once we have a measurement and feedback mechanism that lets
        // us decide based on measurable data.
        sc_do_mount(SNAP_MOUNT_DIR, SNAP_MOUNT_DIR, "none", MS_BIND | MS_REC, NULL);
        sc_do_mount("none", SNAP_MOUNT_DIR, NULL, MS_SHARED | MS_REC, NULL);
    }
}

void sc_setup_user_mounts(struct sc_apparmor *apparmor, int snap_update_ns_fd, const char *snap_name) {
    debug("%s: %s", __FUNCTION__, snap_name);

    char profile_path[PATH_MAX];
    struct stat st;

    sc_must_snprintf(profile_path, sizeof(profile_path), "/var/lib/snapd/mount/snap.%s.user-fstab", snap_name);
    if (stat(profile_path, &st) != 0) {
        // It is ok for the user fstab to not exist.
        return;
    }

    // In our new mount namespace, recursively change all mounts
    // to slave mode, so we see changes from the parent namespace
    // but don't propagate our own changes.
    sc_do_mount("none", "/", NULL, MS_REC | MS_SLAVE, NULL);
    sc_identity old = sc_set_effective_identity(sc_root_group_identity());
    sc_call_snap_update_ns_as_user(snap_update_ns_fd, snap_name, apparmor);
    (void)sc_set_effective_identity(old);
}

void sc_ensure_snap_dir_shared_mounts(void) {
    const char *dirs[] = {SNAP_MOUNT_DIR, "/var/snap", NULL};
    for (int i = 0; dirs[i] != NULL; i++) {
        const char *dir = dirs[i];
        if (!is_mounted_with_shared_option(dir)) {
            /* Since this directory isn't yet shared (but it should be),
             * recursively bind mount it, then recursively share it so that
             * changes to the host are seen in the snap and vice-versa. This
             * allows us to fine-tune propagation events elsewhere for this new
             * mountpoint.
             *
             * Not using MS_SLAVE because it's too late for SNAP_MOUNT_DIR,
             * since snaps are already mounted, and it's not needed for
             * /var/snap.
             */
            sc_do_mount(dir, dir, "none", MS_BIND | MS_REC, NULL);
            sc_do_mount("none", dir, NULL, MS_REC | MS_SHARED, NULL);
        }
    }
}

void sc_setup_parallel_instance_classic_mounts(const char *snap_name, const char *snap_instance_name) {
    char src[PATH_MAX] = {0};
    char dst[PATH_MAX] = {0};

    const char *dirs[] = {SNAP_MOUNT_DIR, "/var/snap", NULL};
    for (int i = 0; dirs[i] != NULL; i++) {
        const char *dir = dirs[i];
        sc_do_mount("none", dir, NULL, MS_REC | MS_SLAVE, NULL);
    }

    /* Mount SNAP_MOUNT_DIR/<snap>_<key> on SNAP_MOUNT_DIR/<snap> */
    sc_must_snprintf(src, sizeof src, "%s/%s", SNAP_MOUNT_DIR, snap_instance_name);
    sc_must_snprintf(dst, sizeof dst, "%s/%s", SNAP_MOUNT_DIR, snap_name);
    sc_do_mount(src, dst, "none", MS_BIND | MS_REC, NULL);

    /* Mount /var/snap/<snap>_<key> on /var/snap/<snap> */
    sc_must_snprintf(src, sizeof src, "/var/snap/%s", snap_instance_name);
    sc_must_snprintf(dst, sizeof dst, "/var/snap/%s", snap_name);
    sc_do_mount(src, dst, "none", MS_BIND | MS_REC, NULL);
}
