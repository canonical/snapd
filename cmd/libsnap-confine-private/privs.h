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

#endif
