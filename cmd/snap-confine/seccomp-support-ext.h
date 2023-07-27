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
#ifndef SNAP_CONFINE_SECCOMP_SUPPORT_EXT_H
#define SNAP_CONFINE_SECCOMP_SUPPORT_EXT_H

#include <linux/filter.h>
#include <stddef.h>

/**
 * Apply a given bpf program as a seccomp system call filter.
 **/
void sc_apply_seccomp_filter(struct sock_fprog *prog);

#endif
