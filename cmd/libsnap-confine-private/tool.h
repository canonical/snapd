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

#ifndef SNAP_CONFINE_TOOL_H
#define SNAP_CONFINE_TOOL_H

/* Forward declaration, for real see apparmor-support.h */
struct sc_apparmor;

/**
 * sc_open_snap_update_ns returns a file descriptor for the snap-update-ns tool.
 **/
int sc_open_snap_update_ns(void);

/**
 * sc_call_snap_update_ns calls snap-update-ns from snap-confine
 **/
void sc_call_snap_update_ns(int snap_update_ns_fd, const char *snap_name, struct sc_apparmor *apparmor);

/**
 * sc_call_snap_update_ns calls snap-update-ns --user-mounts from snap-confine
 **/
void sc_call_snap_update_ns_as_user(int snap_update_ns_fd, const char *snap_name, struct sc_apparmor *apparmor);

/**
 * sc_open_snap_update_ns returns a file descriptor for the snap-discard-ns
 *tool.
 **/
int sc_open_snap_discard_ns(void);

/**
 * sc_call_snap_discard_ns calls the snap-discard-ns from snap confine.
 **/
void sc_call_snap_discard_ns(int snap_discard_ns_fd, const char *snap_name);

#endif
