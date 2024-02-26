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

#ifndef SNAP_CONFINE_ERROR_H
#define SNAP_CONFINE_ERROR_H

#include <stdbool.h>

#define SC_GCC_VERSION (__GNUC__ * 10000 + __GNUC_MINOR__ * 100 + __GNUC_PATCHLEVEL__)

/**
 * The attribute returns_nonnull is only supported by GCC versions >= 4.9.0.
 * Enable building of snap-confine on platforms that are stuck with older
 * GCC versions.
 **/
#if SC_GCC_VERSION >= 40900
#define SC_APPEND_RETURNS_NONNULL , returns_nonnull
#else
#define SC_APPEND_RETURNS_NONNULL
#endif

/**
 * This module defines APIs for simple error management.
 *
 * Errors are allocated objects that can be returned and passed around from
 * functions.  Errors carry a formatted message and optionally a scoped error
 * code. The code is coped with a string "domain" that simply acts as a
 * namespace for various interacting modules.
 **/

/**
 * Error structure.
 **/
typedef struct sc_error {
    // Error domain defines a scope for particular error codes.
    const char *domain;
    // Code differentiates particular errors for the programmer.
    // The code may be zero if the particular meaning is not relevant.
    int code;
    // Message carries a formatted description of the problem.
    char *msg;
} sc_error;

/**
 * Error domain for errors related to system errno.
 **/
#define SC_ERRNO_DOMAIN "errno"

/**
 * Error domain for errors in the libsnap-confine-private library.
 **/
#define SC_LIBSNAP_DOMAIN "libsnap-confine-private"

/** sc_libsnap_error represents distinct error codes used by
 * libsnap-confine-private library. */
typedef enum sc_libsnap_error {
    /** SC_UNSPECIFIED_ERROR indicates an error not worthy of a distinct code.
     */
    SC_UNSPECIFIED_ERROR = 0,
    /** SC_API_MISUSE indicates that public API was called incorrectly. */
    SC_API_MISUSE,
    /** SC_BUG indicates that private API was called incorrectly. */
    SC_BUG,
} sc_libsnap_error;

/**
 * Initialize a new error object.
 *
 * The domain is a cookie-like string that allows the caller to distinguish
 * between "namespaces" of error codes. It should be a static string that is
 * provided by the caller. Both the domain and the error code can be retrieved
 * later.
 *
 * This function calls die() in case of memory allocation failure.
 **/
__attribute__((warn_unused_result, format(printf, 3, 4) SC_APPEND_RETURNS_NONNULL)) sc_error *sc_error_init(
    const char *domain, int code, const char *msgfmt, ...);

/**
 * Initialize an unspecified error with formatted message.
 *
 * This is just syntactic sugar for sc_error_init(SC_LIBSNAP_ERROR,
 * SC_UNSPECIFIED_ERROR, msgfmt, ...) which is repeated often.
 **/
__attribute__((warn_unused_result, format(printf, 1, 2) SC_APPEND_RETURNS_NONNULL)) sc_error *sc_error_init_simple(
    const char *msgfmt, ...);

/**
 * Initialize an API misuse error with formatted message.
 *
 * This is just syntactic sugar for sc_error_init(SC_LIBSNAP_DOMAIN,
 * SC_API_MISUSE, msgfmt, ...) which is repeated often.
 **/
__attribute__((warn_unused_result, format(printf, 1, 2) SC_APPEND_RETURNS_NONNULL)) sc_error *sc_error_init_api_misuse(
    const char *msgfmt, ...);

/**
 * Initialize an errno-based error.
 *
 * The error carries a copy of errno and a custom error message as designed by
 * the caller. See sc_error_init() for a more complete description.
 *
 * This function calls die() in case of memory allocation failure.
 **/
__attribute__((warn_unused_result, format(printf, 2, 3) SC_APPEND_RETURNS_NONNULL)) sc_error *sc_error_init_from_errno(
    int errno_copy, const char *msgfmt, ...);

/**
 * Get the error domain out of an error object.
 *
 * The error domain acts as a namespace for error codes.
 * No change of ownership takes place.
 **/
__attribute__((warn_unused_result SC_APPEND_RETURNS_NONNULL)) const char *sc_error_domain(sc_error *err);

/**
 * Get the error code out of an error object.
 *
 * The error code is scoped by the error domain.
 *
 * An error code of zero is special-cased to indicate that no particular error
 * code is reserved for this error and it's not something that the programmer
 * can rely on programmatically. This can be used to return an error message
 * without having to allocate a distinct code for each one.
 **/
__attribute__((warn_unused_result)) int sc_error_code(sc_error *err);

/**
 * Get the error message out of an error object.
 *
 * The error message is bound to the life-cycle of the error object.
 * No change of ownership takes place.
 **/
__attribute__((warn_unused_result SC_APPEND_RETURNS_NONNULL)) const char *sc_error_msg(sc_error *err);

/**
 * Free an error object.
 *
 * The error object can be NULL.
 **/
void sc_error_free(sc_error *error);

/**
 * Cleanup an error with sc_error_free()
 *
 * This function is designed to be used with
 * __attribute__((cleanup(sc_cleanup_error))).
 **/
__attribute__((nonnull)) void sc_cleanup_error(sc_error **ptr);

/**
 *
 * Die if there's an error.
 *
 * This function is a correct way to die() if the passed error is not NULL.
 *
 * The error message is derived from the data in the error, using the special
 * errno domain to provide additional information if that is available.
 **/
void sc_die_on_error(sc_error *error);

/**
 * Forward an error to the caller.
 *
 * This tries to forward an error to the caller. If this is impossible because
 * the caller did not provide a location for the error to be stored then the
 * sc_die_on_error() is called as a safety measure.
 *
 * Change of ownership takes place and the error is now stored in the recipient.
 *
 * The return value -1 if error is non-NULL and 0 otherwise. The return value
 * makes it convenient to `return sc_error_forward(err_out, err);` as the last
 * line of a function.
 **/
// NOTE: There's no nonnull(1) attribute as the recipient *can* be NULL. With
// the attribute in place GCC optimizes some things out and tests fail.
int sc_error_forward(sc_error **recipient, sc_error *error);

/**
 * Check if a given error matches the specified domain and code.
 *
 * It is okay to match a NULL error, the function simply returns false in that
 * case. The domain cannot be NULL though.
 **/
__attribute__((warn_unused_result)) bool sc_error_match(sc_error *error, const char *domain, int code);

#endif
