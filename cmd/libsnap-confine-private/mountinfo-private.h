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
 */

#ifndef SNAP_CONFINE_MOUNTINFO_PRIVATE_H
#define SNAP_CONFINE_MOUNTINFO_PRIVATE_H

#include "mountinfo.h"

sc_mountinfo_entry *sc_parse_mountinfo_entry(const char *line) __attribute__((nonnull(1)));
void sc_free_mountinfo_entry(sc_mountinfo_entry *entry) __attribute__((nonnull(1)));

#endif
