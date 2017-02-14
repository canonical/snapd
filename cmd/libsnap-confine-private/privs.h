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

/**
 * Initialize privilege control code.
 *
 * This function simply memorizes current user and group identifiers as
 * returned by getuid(2) and getgid(2). The identifiers are kept in private
 * global variables.
 **/
void sc_privs_init();

/**
 * Permanently lower elevated permissions.
 *
 * If the user has elevated permission as a result of running a setuid root
 * application then such permission are permanently lowered.
 *
 * The function ensures that the elevated permission are lowered or dies if
 * this cannot be achieved. Note that only the elevated permissions are
 * lowered. When the process itself was started by root then this function does
 * nothing at all.
 **/
void sc_privs_lower_permanently();

/**
 * Temporarily lower elevated permissions.
 *
 * If the user has elevated permission as a result of running a setuid root
 * application then such permission are temporarily lowered.
 *
 * The function ensures that the elevated permission are lowered or dies if
 * this cannot be achieved. Note that only the elevated permissions are
 * lowered. When the process itself was started by root then this function does
 * nothing at all.
 **/
void sc_privs_lower_temporarily();

/**
 * Raise permissions to elevated level again.
 *
 * This function sets the effective user and group identifiers to 0 (root).
 * The function ensures that the elevated permission are attained or dies if
 * this cannot be achieved.
 *
 * This function should be used in tandem with sc_privs_lower_temporarily.
 **/
void sc_privs_raise();

#endif
