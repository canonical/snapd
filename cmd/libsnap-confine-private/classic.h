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

/**
 * sc_should_use_normal_mode returns true if we should pivot into the base snap.
 *
 * There are two modes of execution for snaps that are not using classic
 * confinement: normal and legacy. The normal mode is where snap-confine sets
 * up a rootfs and then pivots into it using pivot_root(2). The legacy mode is
 * when snap-confine just unshares the initial mount namespace, makes some
 * extra changes but largely runs with what was presented to it initially.
 *
 * Historically the ubuntu-core distribution used the now-legacy mode. This
 * was sensible then since snaps already (kind of) have the right root file-system
 * and just need some privacy and isolation features applied. With the introduction
 * of snaps to classic distributions as well as the introduction of bases, where
 * each snap can use a different root filesystem, this lost sensibility and thus
 * became legacy.
 *
 * For compatibility with current installations of ubuntu-core distributions
 * the legacy mode is used when: the distribution is SC_DISTRO_CORE16 or when
 * the base snap name is not "core" or "ubuntu-core".
 *
 * The SC_DISTRO_CORE16 is applied to systems that boot with the "core",
 * "ubuntu-core" or "core16" snap. Systems using the "core18" base snap do not
 * qualify for that classification.
 **/
bool sc_should_use_normal_mode(sc_distro distro, const char *base_snap_name);

#endif
