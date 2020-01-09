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
	sc_identity id = {.uid = -1,.gid = 0 };
	return id;
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

void write_string_to_file(const char *filepath, const char *buf);

/**
 * Safely create a given directory.
 *
 * The ownership of each created directory depends on the current effective
 * identity of the calling process. Use sc_set_effective_identity() to control
 * it. This approach is used over explicit change of ownership because it is
 * unambiguous in case of existing directories that may have different
 * ownership and mode.
 *
 * This function behaves like "mkdir -p" (recursive mkdir) with the exception
 * that each directory is carefully created in a way that avoids symlink
 * attacks. The preceding directory is kept openat(2) (along with O_DIRECTORY)
 * and the next directory is created using mkdirat(2), this sequence continues
 * while there are more directories to process.
 *
 * NOTE: non-fatal functions don't die on errors. It is the responsibility of
 * the caller to call die() or handle the error appropriately.
 * The function returns -1 in case of any error.
 **/
__attribute__((warn_unused_result))
int sc_nonfatal_mkpath(const char *const path, mode_t mode);

/**
 * sc_mkdir creates a directory if it doesn't exist.
 *
 * The ownership of the directory depends on the current effective identity of
 * the calling process. Use sc_set_effective_identity() to control it. This
 * approach is used over explicit change of ownership because it is unambiguous
 * in case of existing directories that may have different ownership and mode.
 *
 * In addition, it removes a surprise difference in behavior over
 * sc_nonfatal_mkpath(), which creates multiple directories and would have to
 * refrain from changing mode and ownership of existing files to be practical.
 **/
void sc_mkdir(const char *dir, mode_t mode);

/**
 * sc_mksubdir creates a sub-directory if it doesn't exist.
 *
 * The ownership of the directory depends on the current effective identity of
 * the calling process. Use sc_set_effective_identity() to control it. This
 * approach is used over explicit change of ownership because it is unambiguous
 * in case of existing directories that may have different ownership and mode.
 *
 * In addition, it removes a surprise difference in behavior over
 * sc_nonfatal_mkpath(), which creates multiple directories and would have to
 * refrain from changing mode and ownership of existing files to be practical.
 **/
void sc_mksubdir(const char *parent, const char *subdir, mode_t mode);

#endif
