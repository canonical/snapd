/*
 * Copyright (C) 2017 Canonical Ltd
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

#ifndef SNAPD_CMD_SNAP_UPDATE_NS_H
#define SNAPD_CMD_SNAP_UPDATE_NS_H

#define _GNU_SOURCE

#include <stdbool.h>
#include <unistd.h>

extern int bootstrap_errno;
extern const char* bootstrap_msg;

void bootstrap(int argc, char **argv, char **envp);
void process_arguments(int argc, char *const *argv, const char** snap_name_out, bool* should_setns_out, bool* process_user_fstab);
int validate_instance_name(const char* instance_name);

#endif
