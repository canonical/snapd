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

#ifndef SNAP_MOUNT_SUPPORT_PRIVATE_H
#define SNAP_MOUNT_SUPPORT_PRIVATE_H

#include "mount-support.h"

char *__attribute__((used)) get_nextpath(char *path, size_t *offsetp, size_t fulllen);
bool __attribute__((used)) is_subdir(const char *subdir, const char *dir);

#endif
