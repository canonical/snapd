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

int main(int argc, char **argv)
{
	if (argc == 2 && strcmp(argv[1], "--version") == 0) {
		printf("%s %s\n", PACKAGE, PACKAGE_VERSION);
		return 0;
	}
	char *basename = strrchr(argv[0], '/');
	if (basename) {
		debug("setting argv[0] to %s", basename + 1);
		argv[0] = basename + 1;
	}
	if (argc > 1 && !strcmp(argv[0], "ubuntu-core-launcher")) {
		debug("shifting arguments by one");
		argv[1] = argv[0];
		argv++;
		argc--;
	}
	// XXX: replace with pull request 2416 in snapd
	bool classic_confinement = false;
	if (argc > 2 && strcmp(argv[1], "--classic") == 0) {
		classic_confinement = true;
		// Shift remaining arguments left
		int i;
		for (i = 1; i + 1 < argc; ++i) {
			argv[i] = argv[i + 1];
		}
		argv[i] = NULL;
		argc -= 1;
	}
	const int NR_ARGS = 2;
	if (argc < NR_ARGS + 1)
		die("Usage: %s <security-tag> <binary>", argv[0]);

	const char *security_tag = argv[1];
	debug("security tag is %s", security_tag);
	const char *binary = argv[2];
	debug("binary to run is %s", binary);
	uid_t real_uid = getuid();
	gid_t real_gid = getgid();

	if (!verify_security_tag(security_tag))
		die("security tag %s not allowed", security_tag);

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
			const char *group_name = getenv("SNAP_NAME");
			if (group_name == NULL) {
				die("SNAP_NAME is not set");
			}
			sc_initialize_ns_groups();
			struct sc_ns_group *group = NULL;
			group = sc_open_ns_group(group_name, 0);
			sc_lock_ns_mutex(group);
			sc_create_or_join_ns_group(group, &apparmor);
			if (sc_should_populate_ns_group(group)) {
				sc_populate_mount_ns(security_tag);
				sc_preserve_populated_ns_group(group);
			}
			sc_unlock_ns_mutex(group);
			sc_close_ns_group(group);
			// Reset path as we cannot rely on the path from the host OS to
			// make sense. The classic distribution may use any PATH that makes
			// sense but we cannot assume it makes sense for the core snap
			// layout. Note that the /usr/local directories are explicitly
			// left out as they are not part of the core snap.
			debug
			    ("resetting PATH to values in sync with core snap");
			setenv("PATH",
			       "/usr/sbin:/usr/bin:/sbin:/bin:/usr/games", 1);
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
	sc_load_seccomp_context(seccomp_ctx);
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
	// and exec the new binary
	execv(binary, (char *const *)&argv[NR_ARGS]);
	perror("execv failed");
	return 1;
}
