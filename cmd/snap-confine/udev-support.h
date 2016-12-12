/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

#include <stddef.h>

#include <libudev.h>

#define MAX_BUF 1000

struct snappy_udev {
	struct udev *udev;
	struct udev_enumerate *devices;
	struct udev_list_entry *assigned;
	char tagname[MAX_BUF];
	size_t tagname_len;
};

void run_snappy_app_dev_add(struct snappy_udev *udev_s, const char *path);
int snappy_udev_init(const char *security_tag, struct snappy_udev *udev_s);
void snappy_udev_cleanup(struct snappy_udev *udev_s);
void setup_devices_cgroup(const char *security_tag, struct snappy_udev *udev_s);

#endif
