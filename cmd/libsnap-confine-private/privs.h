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

#ifndef SNAP_CONFINE_PRIVS_H
#define SNAP_CONFINE_PRIVS_H

#include <sys/capability.h>

/**
 * Permanently drop elevated permissions.
 *
 * If the user has elevated permission as a result of running a setuid root
 * application then such permission are permanently dropped.
 *
 * The set of dropped permissions include:
 *  - user and group identifier
 *  - supplementary group identifiers
 *
 * The function ensures that the elevated permission are dropped or dies if
 * this cannot be achieved. Note that only the elevated permissions are
 * dropped. When the process itself was started by root then this function does
 * nothing at all.
 **/
void sc_privs_drop(void);

/**
 * Debug print of current capabilities with the provided message prefix.
 */
void sc_debug_capabilities(const char *msg_prefix);

/**
 * Compatibility wrapper around cap_set_ambient().
 */
int sc_cap_set_ambient(cap_value_t cap, cap_flag_value_t set);

/**
 * Compatibility wrapper around cap_reset_ambient().
 */
int sc_cap_reset_ambient(void);

/**
 * Release cap_t allocated through libcap.
 *
 * This function is designed to be used with SC_CLEANUP() macro.
 **/
void sc_cleanup_cap_t(cap_t *ptr);

/**
 * Assert that given caps are listed in the permitted set of the provided,
 * current capability set. The function works like assert() and invokes die()
 * when missing capabilities are found.
 */
void sc_cap_assert_permitted(cap_t current, const cap_value_t caps[], size_t caps_n);

#endif
