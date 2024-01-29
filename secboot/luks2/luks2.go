// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package luks2

// SlotPriority represents the priority of a keyslot.
type SlotPriority int

func (p SlotPriority) String() string {
	switch p {
	case SlotPriorityIgnore:
		return "ignore"
	case SlotPriorityNormal:
		return "normal"
	case SlotPriorityHigh:
		return "prefer"
	default:
		panic("not reached")
	}
}

const (
	// SlotPriorityIgnore means that cryptsetup will not use the associated
	// keyslot unless it is specified explicitly.
	SlotPriorityIgnore SlotPriority = iota

	// SlotPriorityNormal is the default keyslot priority.
	SlotPriorityNormal

	// SlotPriorityHigh means that cryptsetup will try the associated keyslot
	// before it tries any keyslots with a priority of SlotPriorityNormal.
	SlotPriorityHigh
)
