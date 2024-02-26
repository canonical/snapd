/*
 * Copyright (C) 2018 Canonical Ltd
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

#ifndef SNAP_CONFINE_FEATURE_H
#define SNAP_CONFINE_FEATURE_H

#include <stdbool.h>

typedef enum sc_feature_flag {
    SC_FEATURE_PER_USER_MOUNT_NAMESPACE = 1 << 0,
    SC_FEATURE_REFRESH_APP_AWARENESS = 1 << 1,
    SC_FEATURE_PARALLEL_INSTANCES = 1 << 2,
    SC_FEATURE_HIDDEN_SNAP_FOLDER = 1 << 3,
} sc_feature_flag;

/**
 * sc_feature_enabled returns true if a given feature flag has been activated
 * by the user via "snap set core experimental.xxx=true". This is determined by
 * testing the presence of a file in /var/lib/snapd/features/ that is named
 * after the flag name.
 **/
bool sc_feature_enabled(sc_feature_flag flag);

#endif
