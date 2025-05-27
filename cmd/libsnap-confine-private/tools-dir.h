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

#ifndef SNAP_CONFINE_TOOLS_DIR_H
#define SNAP_CONFINE_TOOLS_DIR_H

#include "error.h"

/**
 * Canonical location of tools directory on the host.
 **/
#define SC_CANONICAL_HOST_TOOLS_DIR "/usr/lib/snapd"

/**
 * Alternate location of tools directory on the host.
 **/
#define SC_ALTERNATE_HOST_TOOLS_DIR "/usr/libexec/snapd"

#endif /* SNAP_CONFINE_TOOLS_DIR_H */
