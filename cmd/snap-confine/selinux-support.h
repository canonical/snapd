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
#ifndef SNAP_CONFINE_SELINUX_SUPPORT_H
#define SNAP_CONFINE_SELINUX_SUPPORT_H

/**
 * Set security context for the snap
 *
 * Sets up SELinux context transition to unconfined_service_t.
 **/
int sc_selinux_set_snap_execcon(void);

#endif /* SNAP_CONFINE_SELINUX_SUPPORT_H */
