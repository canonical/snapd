/*
 * Copyright (C) 2015-2017 Canonical Ltd
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
#ifndef SNAP_CONFINE_SECCOMP_SUPPORT_H
#define SNAP_CONFINE_SECCOMP_SUPPORT_H

#include <seccomp.h>

/**
 * Prepare seccomp profile associated with the security tag.
 *
 * This function loads the seccomp profile from
 * /var/lib/snapd/seccomp/profiles/$SECURITY_TAG and stores it into
 * scmp_filter_ctx object.
 *
 * The object is returned to the caller and can be made effective with a call
 * to sc_load_seccomp_context(). The returned value should be cleaned up with
 * seccomp_release().
 *
 * This function calls die() on all errors.
 **/

scmp_filter_ctx sc_prepare_seccomp_context(const char *security_tag);

/**
 * Load a seccomp context.
 *
 * This function calls seccomp_load(3) and handles errors if it fails.
 **/
void sc_load_seccomp_context(scmp_filter_ctx ctx);

/**
 * Release a seccomp context with seccomp_release(3)
 *
 * This function is designed to be used with
 * __attribute__((cleanup(sc_cleanup_seccomp_release))).
 **/
void sc_cleanup_seccomp_release(scmp_filter_ctx * ptr);

#endif
