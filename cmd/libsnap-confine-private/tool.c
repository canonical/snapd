/*
 * Copyright (C) 2018 Canonical Ltd
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

#include "tool.h"

#include <fcntl.h>
#include <libgen.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>

#include "../libsnap-confine-private/apparmor-support.h"
#include "../libsnap-confine-private/cleanup-funcs.h"
#include "../libsnap-confine-private/string-utils.h"
#include "../libsnap-confine-private/utils.h"

/**
 * sc_open_snapd_tool returns a file descriptor of the given internal executable.
 *
 * The executable is located based on the location of the currently executing process.
 * The returning file descriptor can be used with fexecve function, like in sc_call_snapd_tool.
**/
static int sc_open_snapd_tool(const char *tool_name);

/**
 * sc_call_snapd_tool calls a snapd tool by file descriptor.
 *
 * The idea with calling with an open file descriptor is to allow calling executables
 * across mount namespaces, where the executable may not be visible in the new filesystem
 * anymore. The caller establishes an open file descriptor in one namespace and later on
 * performs the call in another mount namespace.
 *
 * The environment vector has special support for expanding the string "SNAPD_DEBUG=x".
 * If such string is present, the "x" is replaced with either "0" or "1" depending on
 * the result of is_sc_debug_enabled().
 **/
static void sc_call_snapd_tool(int tool_fd, const char *tool_name, char **argv,
			       char **envp);

/**
 * sc_call_snapd_tool_with_apparmor calls a snapd tool by file descriptor,
 * possibly confining the program with a specific apparmor profile.
**/
static void sc_call_snapd_tool_with_apparmor(int tool_fd, const char *tool_name,
					     struct sc_apparmor *apparmor,
					     const char *aa_profile,
					     char **argv, char **envp);

int sc_open_snap_update_ns(void)
{
	return sc_open_snapd_tool("snap-update-ns");
}

void sc_call_snap_update_ns(int snap_update_ns_fd, const char *snap_name,
			    struct sc_apparmor *apparmor)
{
	char *snap_name_copy SC_CLEANUP(sc_cleanup_string) = NULL;
	snap_name_copy = sc_strdup(snap_name);

	char aa_profile[PATH_MAX] = { 0 };
	sc_must_snprintf(aa_profile, sizeof aa_profile, "snap-update-ns.%s",
			 snap_name);

	char *argv[] = {
		"snap-update-ns",
		/* This tells snap-update-ns we are calling from snap-confine and locking is in place */
		"--from-snap-confine",
		snap_name_copy, NULL
	};
	char *envp[] = { "SNAPD_DEBUG=x", NULL };

	/* Switch the group to root so that directories, files and locks created by
	 * snap-update-ns are owned by the root group. */
	sc_identity old = sc_set_effective_identity(sc_root_group_identity());
	sc_call_snapd_tool_with_apparmor(snap_update_ns_fd,
					 "snap-update-ns", apparmor,
					 aa_profile, argv, envp);
	(void)sc_set_effective_identity(old);
}

void sc_call_snap_update_ns_as_user(int snap_update_ns_fd,
				    const char *snap_name,
				    struct sc_apparmor *apparmor)
{
	char *snap_name_copy SC_CLEANUP(sc_cleanup_string) = NULL;
	snap_name_copy = sc_strdup(snap_name);

	char aa_profile[PATH_MAX] = { 0 };
	sc_must_snprintf(aa_profile, sizeof aa_profile, "snap-update-ns.%s",
			 snap_name);

	const char *xdg_runtime_dir = getenv("XDG_RUNTIME_DIR");
	char xdg_runtime_dir_env[PATH_MAX + sizeof("XDG_RUNTIME_DIR=")] = { 0 };
	if (xdg_runtime_dir != NULL) {
		sc_must_snprintf(xdg_runtime_dir_env,
				 sizeof(xdg_runtime_dir_env),
				 "XDG_RUNTIME_DIR=%s", xdg_runtime_dir);
	}

	const char *snap_real_home = getenv("SNAP_REAL_HOME");
	char snap_real_home_env[PATH_MAX + sizeof("SNAP_REAL_HOME=")] = { 0 };
	if (snap_real_home != NULL) {
		sc_must_snprintf(snap_real_home_env,
				 sizeof(snap_real_home_env),
				 "SNAP_REAL_HOME=%s", snap_real_home);
	}

	char *argv[] = {
		"snap-update-ns",
		/* This tells snap-update-ns we are calling from snap-confine and locking is in place */
		"--from-snap-confine",
		/* This tells snap-update-ns that we want to process the per-user profile */
		"--user-mounts", snap_name_copy, NULL
	};
	char *envp[] = {
		/* SNAPD_DEBUG=x is replaced by sc_call_snapd_tool_with_apparmor
		 * with either SNAPD_DEBUG=0 or SNAPD_DEBUG=1, see that function
		 * for details. */
		"SNAPD_DEBUG=x",
		xdg_runtime_dir_env,
		snap_real_home_env, NULL
	};
	sc_call_snapd_tool_with_apparmor(snap_update_ns_fd,
					 "snap-update-ns", apparmor,
					 aa_profile, argv, envp);
}

int sc_open_snap_discard_ns(void)
{
	return sc_open_snapd_tool("snap-discard-ns");
}

void sc_call_snap_discard_ns(int snap_discard_ns_fd, const char *snap_name)
{
	char *snap_name_copy SC_CLEANUP(sc_cleanup_string) = NULL;
	snap_name_copy = sc_strdup(snap_name);
	char *argv[] =
	    { "snap-discard-ns", "--from-snap-confine", snap_name_copy, NULL };
	/* SNAPD_DEBUG=x is replaced by sc_call_snapd_tool_with_apparmor with
	 * either SNAPD_DEBUG=0 or SNAPD_DEBUG=1, see that function for details. */
	char *envp[] = { "SNAPD_DEBUG=x", NULL };
	/* Switch the group to root so that directories and locks created by
	 * snap-discard-ns are owned by the root group. */
	sc_identity old = sc_set_effective_identity(sc_root_group_identity());
	sc_call_snapd_tool(snap_discard_ns_fd, "snap-discard-ns", argv, envp);
	(void)sc_set_effective_identity(old);
}

static int sc_open_snapd_tool(const char *tool_name)
{
	// +1 is for the case where the link is exactly PATH_MAX long but we also
	// want to store the terminating '\0'. The readlink system call doesn't add
	// terminating null, but our initialization of buf handles this for us.
	char buf[PATH_MAX + 1] = { 0 };
	if (readlink("/proc/self/exe", buf, sizeof(buf) - 1) < 0) {
		die("cannot readlink /proc/self/exe");
	}
	if (buf[0] != '/') {	// this shouldn't happen, but make sure have absolute path
		die("readlink /proc/self/exe returned relative path");
	}
	// as we are looking up other tools relative to our own path, check
	// we are located where we think we should be - otherwise we
	// may have been hardlink'd elsewhere and then may execute the
	// wrong tool as a result
	if (!sc_is_expected_path(buf)) {
		die("running from unexpected location: %s", buf);
	}
	char *dir_name = dirname(buf);
	int dir_fd SC_CLEANUP(sc_cleanup_close) = -1;
	dir_fd = open(dir_name, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
	if (dir_fd < 0) {
		die("cannot open path %s", dir_name);
	}
	int tool_fd = -1;
	tool_fd = openat(dir_fd, tool_name, O_PATH | O_NOFOLLOW | O_CLOEXEC);
	if (tool_fd < 0) {
		die("cannot open path %s/%s", dir_name, tool_name);
	}
	debug("opened %s executable as file descriptor %d", tool_name, tool_fd);
	return tool_fd;
}

static void sc_call_snapd_tool(int tool_fd, const char *tool_name, char **argv,
			       char **envp)
{
	sc_call_snapd_tool_with_apparmor(tool_fd, tool_name, NULL, NULL, argv,
					 envp);
}

static void sc_call_snapd_tool_with_apparmor(int tool_fd, const char *tool_name,
					     struct sc_apparmor *apparmor,
					     const char *aa_profile,
					     char **argv, char **envp)
{
	debug("calling snapd tool %s", tool_name);
	pid_t child = fork();
	if (child < 0) {
		die("cannot fork to run snapd tool %s", tool_name);
	}
	if (child == 0) {
		/* If the caller provided template environment entry for SNAPD_DEBUG
		 * then expand it to the actual value. */
		for (char **env = envp;
		     /* Mama mia, that's a spicy meatball. */
		     env != NULL && *env != NULL && **env != '\0'; env++) {
			if (sc_streq(*env, "SNAPD_DEBUG=x")) {
				/* NOTE: this is not released, on purpose. */
				char *entry = sc_strdup(*env);
				entry[strlen("SNAPD_DEBUG=x") - 1] =
				    sc_is_debug_enabled()? '1' : '0';
				*env = entry;
			}
		}
		/* Switch apparmor profile for the process after exec. */
		if (apparmor != NULL && aa_profile != NULL) {
			sc_maybe_aa_change_onexec(apparmor, aa_profile);
		}
		fexecve(tool_fd, argv, envp);
		die("cannot execute snapd tool %s", tool_name);
	} else {
		int status = 0;
		debug("waiting for snapd tool %s to terminate", tool_name);
		if (waitpid(child, &status, 0) < 0) {
			die("cannot get snapd tool %s termination status via waitpid", tool_name);
		}
		if (WIFEXITED(status) && WEXITSTATUS(status) != 0) {
			die("%s failed with code %i", tool_name,
			    WEXITSTATUS(status));
		} else if (WIFSIGNALED(status)) {
			die("%s killed by signal %i", tool_name,
			    WTERMSIG(status));
		}
		debug("%s finished successfully", tool_name);
	}
}
