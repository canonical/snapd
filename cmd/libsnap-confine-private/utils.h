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

#include <stdbool.h>
#include <stdlib.h>

/**
 * Macro which calculates array size.
 *
 * Based on ARRAY_SIZE from the Linux kernel, see
 * https://elixir.bootlin.com/linux/v6.13.3/source/include/linux/array_size.h#L11
 */
#define SC_ARRAY_SIZE(arr)                                                                                  \
    (sizeof(arr) / sizeof((arr)[0]) + ((int)sizeof(struct {                                                 \
         _Static_assert(!__builtin_types_compatible_p(typeof(arr), typeof(&(arr)[0])), "must be an array"); \
     })))

__attribute__((noreturn)) __attribute__((format(printf, 1, 2))) void die(const char *fmt, ...);

__attribute__((format(printf, 1, 2))) void debug(const char *fmt, ...);

/**
 * Get an environment variable and convert it to a boolean.
 *
 * Supported values are those of parse_bool(), namely "yes", "no" as well as "1"
 * and "0". All other values are treated as false and a diagnostic message is
 * printed to stderr. If the environment variable is unset, set value to the
 * default_value as if the environment variable was set to default_value.
 **/
bool getenv_bool(const char *name, bool default_value);

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
 * Return true if executing inside a container.
 **/
bool sc_is_in_container(void);

/**
 * sc_identity describes the user performing certain operation.
 *
 * UID and GID represent user and group accounts numbers and are controlled by
 * change_uid and change_gid flags.
 **/
typedef struct sc_identity {
    uid_t uid;
    gid_t gid;
    bool change_uid : 1;
    bool change_gid : 1;
} sc_identity;

/**
 * Identity of the root group.
 *
 * The return value is suitable for passing to sc_set_effective_identity. It
 * causes the effective group to change to the root group. No change is made to
 * effective user identity.
 **/
static inline sc_identity sc_root_group_identity(void) {
    sc_identity id = {
        /* Explicitly set our intent of changing just the GID.
         * Refactoring of this code must retain this property. */
        .change_uid = false,
        .change_gid = true,
        .gid = 0,
    };
    return id;
}

/**
 * Produce value indicating no change in current identity.
 *
 * Produce a value of sc_identity which indicates no change in the identity of
 * the current process.
 **/
static inline sc_identity sc_no_change_identity(void) {
    sc_identity id = {
        /* Explicit no change in either uid or gid. */
        .change_uid = false,
        .change_gid = false,
    };
    return id;
}

/**
 * Set the effective user and group IDs to given values.
 *
 * Effective user and group identifiers are applied to the system. The
 * current values are returned as another identity that can be restored via
 * another call to sc_set_effective_identity.
 *
 * The fields change_uid and change_gid control if user and group ID is changed.
 * The returned old identity has identical values of both use flags.
 **/
sc_identity sc_set_effective_identity(sc_identity identity);

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
 * The directory will be owned by the given user and group, unless these
 * parameters are set to -1 (in which case they are not altered).
 *
 * The function returns -1 in case of any error.
 **/
__attribute__((warn_unused_result)) int sc_nonfatal_mkpath(const char *const path, mode_t mode, uid_t uid, uid_t gid);

/**
 * Return true if path is a valid path for the snap-confine binary
 **/
__attribute__((warn_unused_result)) bool sc_is_expected_path(const char *path);

/**
 * Wait for file to appear for timeout_sec seconds. Returns true once the file
 * is present.
 */
bool sc_wait_for_file(const char *path, size_t timeout_sec);

/**
 * Ensure a directory exists inside a given parent directory. Essentially a
 * convenience wrapper around mkdirat() followed by fchownat(), if a new
 * directory was created.
 *
 * Returns -1 in case of error.
 */
__attribute__((warn_unused_result)) int sc_ensure_mkdirat(int fd, const char *name, mode_t mode, uid_t uid, uid_t gid);

/**
 * Ensure a directory exists. Essentially a convenience wrapper around mkdirat()
 * followed by chown() if a new directory was created.
 *
 * Returns -1 in case of error.
 */
__attribute__((warn_unused_result)) int sc_ensure_mkdir(const char *name, mode_t mode, uid_t uid, uid_t gid);

#endif
