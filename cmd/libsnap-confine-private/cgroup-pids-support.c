/*
 * Copyright (C) 2019 Canonical Ltd
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

#include "cgroup-pids-support.h"

#include "cgroup-support.h"

static const char *pids_cgroup_dir = "/sys/fs/cgroup/pids";

void sc_cgroup_pids_join(const char *snap_security_tag, pid_t pid) {
    sc_cgroup_create_and_join(pids_cgroup_dir, snap_security_tag, pid);
}
