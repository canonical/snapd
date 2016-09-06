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

#include "sc-main.h"
#include "config.h"

#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <unistd.h>
#ifdef HAVE_APPARMOR
#include <sys/apparmor.h>
#endif				// ifdef HAVE_APPARMOR

#include "classic.h"
#include "mount-support.h"
#include "snap.h"
#include "utils.h"
#ifdef HAVE_SECCOMP
#include "seccomp-support.h"
#endif				// ifdef HAVE_SECCOMP
#include "udev-support.h"
#include "cleanup-funcs.h"
#include "user-support.h"
#include "quirks.h"

int sc_main(int argc, char **argv)
{
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

	// this code always needs to run as root for the cgroup/udev setup,
	// however for the tests we allow it to run as non-root
	if (geteuid() != 0 && secure_getenv("SNAP_CONFINE_NO_ROOT") == NULL) {
		die("need to run as root or suid");
	}
#ifdef HAVE_SECCOMP
	scmp_filter_ctx seccomp_ctx
	    __attribute__ ((cleanup(sc_cleanup_seccomp_release))) = NULL;
	seccomp_ctx = sc_prepare_seccomp_context(security_tag);
#endif				// ifdef HAVE_SECCOMP

	if (geteuid() == 0) {
		sc_unshare_mount_ns();
		sc_populate_mount_ns(security_tag);
		struct snappy_udev udev_s;
		if (snappy_udev_init(security_tag, &udev_s) == 0)
			setup_devices_cgroup(security_tag, &udev_s);
		snappy_udev_cleanup(&udev_s);

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

	// https://wiki.ubuntu.com/SecurityTeam/Specifications/SnappyConfinement
#ifdef HAVE_APPARMOR
	int rc = 0;
	// set apparmor rules
	rc = aa_change_onexec(security_tag);
	if (rc != 0) {
		if (secure_getenv("SNAPPY_LAUNCHER_INSIDE_TESTS") == NULL)
			die("aa_change_onexec failed with %i", rc);
	}
#endif				// ifdef HAVE_APPARMOR
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
