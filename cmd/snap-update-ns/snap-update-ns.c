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

#ifdef HAVE_CONFIG_H
#include "config.h"
#endif

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <mntent.h>
#include <sched.h>
#include <stdio.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/vfs.h>
#include <unistd.h>

#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/error.h"
#include "../libsnap-confine-private/mountinfo.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"
#include "mount-entry.h"

static void missing_locking()
{
	fprintf(stderr, "XXX: snap-alter-ns doesn't lock"
		" the mount namespace yet.\n");
}

static void sc_reassociate_with_snap_namespace_or_exit(const char *snap_name);
static bool sc_should_act_on_change(const struct sc_mount_change *change);

#define SC_DESIRED_PROFILE_FMT "/var/lib/snapd/mount/snap.%s.fstab"
#define SC_CURRENT_PROFILE_FMT "/run/snapd/ns/%s.fstab"
#define SC_MNT_NS_FMT          "/run/snapd/ns/%s.mnt"

int main(int argc, char **argv)
{
	if (argc != 2) {
		printf("Usage: snap-alter-ns SNAP-NAME");
		return 1;
	}
	const char *snap_name = argv[1];
	// TODO: verify once verify_snap_name lands.

	debug("Checking if the mount namespace of snap %s needs changes",
	      snap_name);

	// TODO: use locking from ns-support.
	//
	// This ensures we see consistent "current" and "desired" profiles.
	//
	// The current profile is modified by snap-discard-ns, snap-alter-ns and
	// snap-confine. All the tools follow the locking system.
	//
	// The desired profile is modified by snapd. Snapd runs snap-alter-ns we
	// put the burden of not clobbering this file while we may be reading.
	missing_locking();

	// The desired profile is stored in /var/lib/snapd/mount/$SNAP_NAME.fstab
	// The current profile is stored in /run/snapd/ns/$SNAP_NAME.fstab
	//
	// We are loading both to compare them and compute what needs to be done to
	// alter the namespace to match the desired profile.
	char buf[PATH_MAX];
	struct sc_mount_entry
	    __attribute__ ((cleanup(sc_cleanup_mount_entry_list))) * desired =
	    NULL;
	sc_must_snprintf(buf, sizeof buf, SC_DESIRED_PROFILE_FMT, snap_name);
	desired = sc_load_mount_profile(buf);
	debug("Loaded desired mount profile:");
	for (struct sc_mount_entry * e = desired; e != NULL; e = e->next) {
		debug("\tdesired: %s", sc_mount_entry_to_string(e));
	}
	if (desired == NULL) {
		debug("\tdesired: (empty profile)");
	}

	struct sc_mount_entry
	    __attribute__ ((cleanup(sc_cleanup_mount_entry_list))) * current =
	    NULL;
	sc_must_snprintf(buf, sizeof buf, SC_CURRENT_PROFILE_FMT, snap_name);
	current = sc_load_mount_profile(buf);
	debug("Loaded current mount profile");
	for (struct sc_mount_entry * e = current; e != NULL; e = e->next) {
		debug("\tcurrent: %s", sc_mount_entry_to_string(e));
	}
	if (current == NULL) {
		debug("\tcurrent: (empty profile)");
	}

	if (current == NULL && desired == NULL) {
		debug("There's nothing to do");
		return 0;
	}
	// TODO: correct the wiki, we don't quit if something is not present as
	// this is a valid case as well (e.g. a profile gets removed).

	// Sort both profiles so that we can compare them more easily.
	sc_sort_mount_entries(&desired);
	sc_sort_mount_entries(&current);

	// XXX: at this point we re-associate with the mount namespace of $SNAP_NAME
	// or we quit if no such namespace exists. After this function returns the
	// current working directory is / and we are in the right place to perform
	// modifications (mount and unmount things).
	sc_reassociate_with_snap_namespace_or_exit(snap_name);
	debug("Joined the mount namespace of the snap %s", snap_name);

	// The current and desired profiles are then now compared. Each entry that
	// doesn't exist in the current profile but exists in the desired results
	// with a mount operation. Each entry that doesn't exist in the desired
	// profile but exists in the current profile results in an unmount
	// operation. All unmount operations are performed first, before the first
	// mount operation.
	// NOTE: The initial pointers are preserved as we want to free them later.
	// The two values below just help us track state for the comparison
	// algorithm.
	struct sc_mount_entry *c = current;
	struct sc_mount_entry *d = desired;
	struct sc_mount_change change;
	debug("Looking for necessary changes to the mount namespace.");
	int num_changed = 0;
	int num_skipped = 0;
	while (c != NULL || d != NULL) {
		// NOTE: tricky, don't swap the arguments :)
		sc_compute_required_mount_changes(&d, &c, &change);
		switch (change.action) {
		case SC_ACTION_NONE:
			continue;
		case SC_ACTION_MOUNT:
			debug("\t(should mount) %s",
			      sc_mount_entry_to_string(change.entry));
			break;
		case SC_ACTION_UNMOUNT:
			debug("\t(should unmount) %s", change.entry->mnt_dir);
			break;
		}
		if (sc_should_act_on_change(&change)) {
			debug("\tActing on the change...");
			sc_act_on_mount_change(&change);
			num_changed += 1;
		} else {
			debug("\tNot acting on the change!");
			num_skipped += 1;
		}
	}
	if (num_skipped > 0) {
		debug("Mount namespace of snap %s has not been fully altered.",
		      snap_name);
		debug("Number of changes skipped: %d", num_skipped);
		debug("snap-alter-ns does mount over existing mount points.");
	}
	if (num_changed > 0) {
		debug("Mount namespace of snap %s has been altered.",
		      snap_name);
		debug("Number of changes applied: %d", num_changed);

		// Once all mount operations are performed the current profile is
		// overwritten with the desired profile.
		// This way the next time we are called we will have nothing to do.
		sc_must_snprintf(buf, sizeof buf, SC_CURRENT_PROFILE_FMT,
				 snap_name);
		sc_save_mount_profile(desired, buf);
		debug("The current profile has been updated.");
	}
	if (num_skipped == 0 && num_changed == 0) {
		debug("Mount namespace of snap %s is already up-to-date.",
		      snap_name);
	}
	return 0;
}

static void sc_show_mountinfo(struct sc_mountinfo_entry *mi_entry)
{
	debug("\t\tid:           %d", sc_mountinfo_entry_mount_id(mi_entry));
	debug("\t\tparent-id:    %d", sc_mountinfo_entry_parent_id(mi_entry));
	debug("\t\troot:         %s", sc_mountinfo_entry_root(mi_entry));
	debug("\t\tmount-dir:    %s", sc_mountinfo_entry_mount_dir(mi_entry));
	debug("\t\tmount-opts:   %s", sc_mountinfo_entry_mount_opts(mi_entry));
	debug("\t\toptional:     %s",
	      sc_mountinfo_entry_optional_fields(mi_entry));
	debug("\t\tfs-type:      %s", sc_mountinfo_entry_fs_type(mi_entry));
	debug("\t\tmount-source: %s",
	      sc_mountinfo_entry_mount_source(mi_entry));
	debug("\t\tsuper-opts:   %s", sc_mountinfo_entry_super_opts(mi_entry));
}

static struct sc_mountinfo_entry *sc_find_mountinfo_by_id(struct sc_mountinfo
							  *mi, int mount_id)
{
	for (struct sc_mountinfo_entry * mi_entry =
	     sc_first_mountinfo_entry(mi); mi_entry != NULL;
	     mi_entry = sc_next_mountinfo_entry(mi_entry)) {
		if (sc_mountinfo_entry_mount_id(mi_entry) == mount_id) {
			return mi_entry;
		}
	}
	return NULL;
}

static bool sc_should_act_on_change(const struct sc_mount_change *change)
{
	// Load the table of mount points that affect the current process.  We're
	// doing this each time we are asked to mount something as it is safer than
	// trying to keep track of what the kernel may be doing.
	struct sc_mountinfo
	    __attribute__ ((cleanup(sc_cleanup_mountinfo))) * mi = NULL;
	const char *mnt_dir;
	mi = sc_parse_mountinfo(NULL);

	switch (change->action) {
	case SC_ACTION_MOUNT:
		// We cannot mount over existing mount points as that can confuse
		// apparmor. As a safety measure we reject such mount requests.
		for (struct sc_mountinfo_entry * mi_entry =
		     sc_first_mountinfo_entry(mi); mi_entry != NULL;
		     mi_entry = sc_next_mountinfo_entry(mi_entry)) {
			mnt_dir = sc_mountinfo_entry_mount_dir(mi_entry);
			// XXX: it would be perfect if this could detect that we don't have
			// to do anything but it is not an error. Specifically for the case
			// of bind mounts that are already satisfied.
			if (strcmp(mnt_dir, change->entry->mnt_dir) == 0) {
				debug("\tIgnoring request to mount over"
				      " an existing mount-point: %s",
				      change->entry->mnt_dir);
				debug("\tIn the way:");
				sc_show_mountinfo(mi_entry);
				struct sc_mountinfo_entry *parent_mi_entry =
				    sc_find_mountinfo_by_id(mi,
							    sc_mountinfo_entry_parent_id
							    (mi_entry));
				while (parent_mi_entry != NULL) {
					debug("\t(parent chain)...");
					sc_show_mountinfo(parent_mi_entry);
					parent_mi_entry =
					    sc_find_mountinfo_by_id(mi,
								    sc_mountinfo_entry_parent_id
								    (parent_mi_entry));
				}
				return false;
			}
		}
		return true;
	case SC_ACTION_UNMOUNT:
		// We don't want to unmount something that is not mounted.
		for (struct sc_mountinfo_entry * mi_entry =
		     sc_first_mountinfo_entry(mi); mi_entry != NULL;
		     mi_entry = sc_next_mountinfo_entry(mi_entry)) {
			mnt_dir = sc_mountinfo_entry_mount_dir(mi_entry);
			if (strcmp(mnt_dir, change->entry->mnt_dir) == 0) {
				return true;
			}
		}
		debug("\tIgnoring request to unmount something that"
		      " is not mounted: %s", change->entry->mnt_dir);
		return false;
	default:
		// Just in case.
		return false;
	}
}

static void sc_reassociate_with_snap_namespace_or_exit(const char *snap_name)
{
	char buf[PATH_MAX];
	int mnt_ns_fd __attribute__ ((cleanup(sc_cleanup_close))) = -1;

	sc_must_snprintf(buf, sizeof buf, SC_MNT_NS_FMT, snap_name);

	mnt_ns_fd = open(buf, O_RDONLY | O_NOFOLLOW | O_CLOEXEC);
	if (mnt_ns_fd < 0) {
		// If the namespace file does not exist then there is nothing to do.
		if (errno == ENOENT) {
			debug("there is no mount namespace for snap %s,"
			      " (no file)", snap_name);
			exit(0);
		}
		die("cannot open mount namespace of snap %s", snap_name);
	}
	// If the mount namespace file exists but is not a bound mount namespace
	// then it must have been discarded earlier and there is nothing to do.
	struct statfs stat_buf;
	if (fstatfs(mnt_ns_fd, &stat_buf) < 0) {
		die("cannot perform fstatfs() on an mount namespace file descriptor");
	}
#ifndef NSFS_MAGIC
	// Account for kernel headers old enough to not know about NSFS_MAGIC.
#define NSFS_MAGIC 0x6e736673
#endif
	if (stat_buf.f_type != NSFS_MAGIC) {
		debug("there's no preserved mount namespace for %s,"
		      " (no bind mount)", snap_name);
		exit(0);
	}
	// Associate with the mount namespace of the snap in question.
	if (setns(mnt_ns_fd, CLONE_NEWNS) < 0) {
		die("cannot re-associate with mount namespace of snap %s",
		    snap_name);
	}
}
