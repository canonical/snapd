// -*- Mode: Go; indent-tabs-mode: t -*-

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

package snap

import (
	"fmt"
)

var implicitSlots = []string{
	"firewall-control",
	"home",
	"locale-control",
	"log-observe",
	"mount-observe",
	"network",
	"network-bind",
	"network-control",
	"network-observe",
	"snap-control",
	"system-observe",
	"timeserver-control",
	"timezone-control",
	// XXX: those two should perhaps not be added by default
	"unity7",
	"x",
}

// AddCommonSlotsToOSSnap adds slots of well-known interfaces to the OS snap.
//
// This function is indented to be used temporarily, before the OS snap is
// updated to contain appropriate slot definitions.
//
// It is assumed that slots have names matching the interface name. Existing
// slots are not changed, only missing slots are added.
func AddCommonSlotsToOSSnap(snapInfo *Info) error {
	if snapInfo.Type != TypeOS {
		return fmt.Errorf("common slots can only be added to the OS snap")
	}
	for _, ifaceName := range implicitSlots {
		if _, ok := snapInfo.Slots[ifaceName]; !ok {
			snapInfo.Slots[ifaceName] = &SlotInfo{
				Snap:      snapInfo,
				Interface: ifaceName,
			}
		}
	}
	return nil
}
