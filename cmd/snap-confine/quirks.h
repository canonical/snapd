/*
 * Copyright (C) 2016 Canonical Ltd
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

#ifndef SNAP_QUIRKS_H
#define SNAP_QUIRKS_H

/**
 * Setup various quirks that have to exists for now.
 *
 * This function applies non-standard tweaks that are required
 * because of requirement to stay compatible with certain snaps
 * that were tested with pre-chroot layout.
 **/
void sc_setup_quirks(void);

#endif
