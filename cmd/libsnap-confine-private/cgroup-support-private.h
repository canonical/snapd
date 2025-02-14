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

#ifndef SC_CGROUP_SUPPORT_PRIVATE_H
#define SC_CGROUP_SUPPORT_PRIVATE_H

#include "cgroup-support.h"

void sc_set_cgroup_root(const char *dir);

const char *sc_get_default_cgroup_root(void);

void sc_set_self_cgroup_path(const char *path);

const char *sc_get_default_self_cgroup_path(void);

#endif
