/*
 * Copyright (C) 2025 Canonical Ltd
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
#ifndef SNAP_CONFINE_LOCKING_PRIVATE_H
#define SNAP_CONFINE_LOCKING_PRIVATE_H

#include "locking.h"

int sc_lock_generic(const char *scope, uid_t uid);

// Set alternate inhibit directory
void sc_set_inhibit_dir(const char *dir);

const char *sc_get_default_inhibit_dir(void);

// Set alternate locking directory
void sc_set_lock_dir(const char *dir);

const char *sc_get_default_lock_dir(void);

#endif  // SNAP_CONFINE_LOCKING_PRIVATE_H
