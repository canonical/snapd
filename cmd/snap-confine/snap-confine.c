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

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include "../libsnap-confine-private/classic.h"
#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/locking.h"
#include "../libsnap-confine-private/secure-getenv.h"
#include "../libsnap-confine-private/snap.h"
#include "../libsnap-confine-private/utils.h"
#include "apparmor-support.h"
#include "mount-support.h"
#include "ns-support.h"
#include "quirks.h"
#ifdef HAVE_SECCOMP
#include "seccomp-support.h"
#endif				// ifdef HAVE_SECCOMP
#include "udev-support.h"
#include "user-support.h"
#include "snap-confine-args.h"

int main(int argc, char **argv)
{
	// Use our super-defensive parser to figure out what we've been asked to do.
	struct sc_error *err = NULL;
	struct sc_args *args __attribute__ ((cleanup(sc_cleanup_args))) = NULL;
	args = sc_nonfatal_parse_args(&argc, &argv, &err);
	sc_die_on_error(err);

	// We've been asked to print the version string so let's just do that.
	if (sc_args_is_version_query(args)) {
		printf("%s %s\n", PACKAGE, PACKAGE_VERSION);
		return 0;
	}
	// Collect and validate the security tag and a few other things passed on
	// command line.
	const char *security_tag = sc_args_security_tag(args);
	if (!verify_security_tag(security_tag)) {
		die("security tag %s not allowed", security_tag);
	}
	const char *executable = sc_args_executable(args);
	bool classic_confinement = sc_args_is_classic_confinement(args);

	const char *snap_name = getenv("SNAP_NAME");
	if (snap_name == NULL) {
		die("SNAP_NAME is not set");
	}
	sc_snap_name_validate(snap_name, NULL);

	debug("security tag: %s", security_tag);
	debug("executable:   %s", executable);
	debug("confinement:  %s",
	      classic_confinement ? "classic" : "non-classic");

	// Who are we?
	uid_t real_uid = getuid();
	gid_t real_gid = getgid();

#ifndef CAPS_OVER_SETUID
	// this code always needs to run as root for the cgroup/udev setup,
	// however for the tests we allow it to run as non-root
	if (geteuid() != 0 && secure_getenv("SNAP_CONFINE_NO_ROOT") == NULL) {
		die("need to run as root or suid");
	}
#endif
	struct sc_apparmor apparmor;
	sc_init_apparmor_support(&apparmor);
	if (!apparmor.is_confined && apparmor.mode != SC_AA_NOT_APPLICABLE
	    && getuid() != 0 && geteuid() == 0) {
		// Refuse to run when this process is running unconfined on a system
		// that supports AppArmor when the effective uid is root and the real
		// id is non-root.  This protects against, for example, unprivileged
		// users trying to leverage the snap-confine in the core snap to
		// escalate privileges.
		die("snap-confine has elevated permissions and is not confined"
		    " but should be. Refusing to continue to avoid"
		    " permission escalation attacks");
	}
	// TODO: check for similar situation and linux capabilities.
#ifdef HAVE_SECCOMP
	scmp_filter_ctx seccomp_ctx
	    __attribute__ ((cleanup(sc_cleanup_seccomp_release))) = NULL;
	seccomp_ctx = sc_prepare_seccomp_context(security_tag);
#endif				// ifdef HAVE_SECCOMP

	if (geteuid() == 0) {
		if (classic_confinement) {
			/* 'classic confinement' is designed to run without the sandbox
			 * inside the shared namespace. Specifically:
			 * - snap-confine skips using the snap-specific mount namespace
			 * - snap-confine skips using device cgroups
			 * - snapd sets up a lenient AppArmor profile for snap-confine to use
			 * - snapd sets up a lenient seccomp profile for snap-confine to use
			 */
			debug
			    ("skipping sandbox setup, classic confinement in use");
		} else {
			/* snap-confine uses privately-shared /run/snapd/ns to store
			 * bind-mounted mount namespaces of each snap. In the case that
			 * snap-confine is invoked from the mount namespace it typically
			 * constructs, the said directory does not contain mount entries
			 * for preserved namespaces as those are only visible in the main,
			 * outer namespace.
			 *
			 * In order to operate in such an environment snap-confine must
			 * first re-associate its own process with another namespace in
			 * which the /run/snapd/ns directory is visible.  The most obvious
			 * candidate is pid one, which definitely doesn't run in a
			 * snap-specific namespace, has a predictable PID and is long
			 * lived.
			 */
			sc_reassociate_with_pid1_mount_ns();
			// Do global initialization:
			int global_lock_fd = sc_lock_global();
			// ensure that "/" or "/snap" is mounted with the
			// "shared" option, see LP:#1668659
			debug("ensuring that snap mount directory is shared");
			sc_ensure_shared_snap_mount();
			debug("unsharing snap namespace directory");
			sc_initialize_ns_groups();
			sc_unlock_global(global_lock_fd);

			// Do per-snap initialization.
			int snap_lock_fd = sc_lock(snap_name);
			debug("initializing mount namespace: %s", snap_name);
			struct sc_ns_group *group = NULL;
			group = sc_open_ns_group(snap_name, 0);
			sc_create_or_join_ns_group(group, &apparmor);
			if (sc_should_populate_ns_group(group)) {
				sc_populate_mount_ns(snap_name);
				sc_preserve_populated_ns_group(group);
			}
			sc_close_ns_group(group);
			sc_unlock(snap_name, snap_lock_fd);

			// Reset path as we cannot rely on the path from the host OS to
			// make sense. The classic distribution may use any PATH that makes
			// sense but we cannot assume it makes sense for the core snap
			// layout. Note that the /usr/local directories are explicitly
			// left out as they are not part of the core snap.
			debug
			    ("resetting PATH to values in sync with core snap");
			setenv("PATH",
			       "/usr/local/sbin:"
			       "/usr/local/bin:"
			       "/usr/sbin:"
			       "/usr/bin:"
			       "/sbin:"
			       "/bin:" "/usr/games:" "/usr/local/games", 1);
			// Ensure we set the various TMPDIRs to /tmp.
			// One of the parts of setting up the mount namespace is to create a private /tmp
			// directory (this is done in sc_populate_mount_ns() above). The host environment
			// may point to a directory not accessible by snaps so we need to reset it here.
			const char *tmpd[] = { "TMPDIR", "TEMPDIR", NULL };
			int i;
			for (i = 0; tmpd[i] != NULL; i++) {
				if (setenv(tmpd[i], "/tmp", 1) != 0) {
					die("cannot set environment variable '%s'", tmpd[i]);
				}
			}
			struct snappy_udev udev_s;
			if (snappy_udev_init(security_tag, &udev_s) == 0)
				setup_devices_cgroup(security_tag, &udev_s);
			snappy_udev_cleanup(&udev_s);
		}
		// The rest does not so temporarily drop privs back to calling
		// user (we'll permanently drop after loading seccomp)
		if (setegid(real_gid) != 0)
			die("setegid failed");
		if (seteuid(real_uid) != 0)
			die("seteuid failed");

		if (real_gid != 0 && geteuid() == 0)
			die("dropping privs did not work");
		if (real_uid != 0 && getegid() == 0)
			die("dropping privs did not work");
	}
	// Ensure that the user data path exists.
	setup_user_data();
#if 0
	setup_user_xdg_runtime_dir();
#endif

	// https://wiki.ubuntu.com/SecurityTeam/Specifications/SnappyConfinement
	sc_maybe_aa_change_onexec(&apparmor, security_tag);
#ifdef HAVE_SECCOMP
	if (getenv("SNAP_CONFINE_DUMP_OLD_SECCOMP") != NULL)
		seccomp_export_pfc(seccomp_ctx, 1 /*stdout */ );
	sc_apply_seccomp_bpf(security_tag);
#endif				// ifdef HAVE_SECCOMP

	// Permanently drop if not root
	if (geteuid() == 0) {
		// Note that we do not call setgroups() here because its ok
		// that the user keeps the groups he already belongs to
		if (setgid(real_gid) != 0)
			die("setgid failed");
		if (setuid(real_uid) != 0)
			die("setuid failed");

		if (real_gid != 0 && (getuid() == 0 || geteuid() == 0))
			die("permanently dropping privs did not work");
		if (real_uid != 0 && (getgid() == 0 || getegid() == 0))
			die("permanently dropping privs did not work");
	}
	// and exec the new executable
	argv[0] = (char *)executable;
	debug("execv(%s, %s...)", executable, argv[0]);
	for (int i = 1; i < argc; ++i) {
		debug(" argv[%i] = %s", i, argv[i]);
	}
	execv(executable, (char *const *)&argv[0]);
	perror("execv failed");
	return 1;
}
