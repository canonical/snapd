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
#ifndef CORE_LAUNCHER_UTILS_H
#define CORE_LAUNCHER_UTILS_H

#include <stdlib.h>
#include <stdbool.h>

__attribute__((noreturn))
    __attribute__((format(printf, 1, 2)))
void die(const char *fmt, ...);

__attribute__((format(printf, 1, 2)))
void debug(const char *fmt, ...);

/**
 * Return true if debugging is enabled.
 *
 * This can used to avoid costly computation that is only useful for debugging.
 **/
bool sc_is_debug_enabled(void);

/**
 * Return true if re-execution is enabled.
 **/
bool sc_is_reexec_enabled(void);

/**
 * sc_identity describes the user performing certain operation.
 *
 * UID and GID may represent actual user and group accounts. Either value may
 * also be set to -1 to indicate that a specific identity change should not
 * be performed.
**/
typedef struct sc_identity {
	uid_t uid;
	gid_t gid;
} sc_identity;

static inline sc_identity sc_root_group_identity(void)
{
	return (sc_identity) {
	.uid = -1,.gid = 0};
}

/**
 * Set the effective user and group IDs to given values.
 *
 * Effective user and group identifiers are applied to the system. The
 * current values are returned as another identity that can be restored via
 * another call to sc_set_effective_identity.
 *
 * If -1 is used as either user or group ID then the respective change is not
 * made and the returned old identity will also use -1 as that value.
**/
sc_identity sc_set_effective_identity(sc_identity identity);

/**
 * sc_ownership describes the ownership of filesystem objects.
 *
 * Ownership is influenced by identity used during the operation.
 * Typically either identity is unchanged and ownership is explicit
 * or identity is explicit and ownership is implied.
 **/
typedef struct sc_ownership {
	uid_t uid;
	gid_t gid;
} sc_ownership;

static inline sc_ownership sc_root_ownership(void)
{
	return (sc_ownership) {
	.uid = 0,.gid = 0};
}

static inline sc_ownership sc_unchanged_ownership(void)
{
	return (sc_ownership) {
	.uid = -1,.gid = -1};
}

void write_string_to_file(const char *filepath, const char *buf);

/**
 * Safely create a given directory.
 *
 * NOTE: non-fatal functions don't die on errors. It is the responsibility of
 * the caller to call die() or handle the error appropriately.
 *
 * This function behaves like "mkdir -p" (recursive mkdir) with the exception
 * that each directory is carefully created in a way that avoids symlink
 * attacks. The preceding directory is kept openat(2) (along with O_DIRECTORY)
 * and the next directory is created using mkdirat(2), this sequence continues
 * while there are more directories to process.
 *
 * The function returns -1 in case of any error.
 **/
__attribute__((warn_unused_result))
int sc_nonfatal_mkpath(const char *const path, mode_t mode,
		       sc_ownership ownership);

/**
 * sc_mkdir creates a directory with given mode and owner.
 *
 * If the directory exists it is only modified to posses the desired ownership
 * and permissions. If necessary it is created in a way that prevents non-root
 * users from opening it before the owner is switched to the desired values.
 **/
void sc_mkdir(const char *dir, mode_t mode, sc_ownership ownership);

/**
 * sc_mksubdir creates a sub-directory with a given mode and owner.
 *
 * If the sub-directory exists it is only modified to posses the desired
 * ownership and permissions. If necessary it is created in a way that prevents
 * non-root users from opening it before the owner is switched to the desired
 * values.
 **/
void sc_mksubdir(const char *parent, const char *subdir, mode_t mode,
		 sc_ownership ownership);

#endif
