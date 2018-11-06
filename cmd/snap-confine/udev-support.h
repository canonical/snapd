/*
 * Copyright (C) 2015-2020 Canonical Ltd
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

#ifndef SNAP_CONFINE_UDEV_SUPPORT_H
#define SNAP_CONFINE_UDEV_SUPPORT_H

typedef enum {
	/* Require device cgroup, even if no devices are assigned to the snap */
	SC_DEVICE_CGROUP_MODE_REQUIRED = 0x0,
	/* Device cgroup is optional if no devices are assigned to the snap. This is
	 * to comply with the legacy behavior */
	SC_DEVICE_CGROUP_MODE_OPTIONAL = 0x1,
} sc_device_cgroup_mode;

void sc_setup_device_cgroup(const char *security_tag,
			    sc_device_cgroup_mode mode);

#endif
