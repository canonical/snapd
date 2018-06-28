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
#ifndef SNAP_CONFINE_CLASSIC_H
#define SNAP_CONFINE_CLASSIC_H

#include <stdbool.h>

// Location of the host filesystem directory in the core snap.
#define SC_HOSTFS_DIR "/var/lib/snapd/hostfs"

typedef enum sc_distro {
	SC_DISTRO_CORE16,	// As present in both "core" and later on in "core16"
	SC_DISTRO_CORE_OTHER,	// Any core distribution.
	SC_DISTRO_CLASSIC,	// Any classic distribution.
} sc_distro;

sc_distro sc_classify_distro(void);

bool sc_should_use_normal_mode(sc_distro distro, const char *base_snap_name);

#endif
