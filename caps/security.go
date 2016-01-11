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

package caps

// securitySystem abstracts the implementation details of a given security
// system (syccomp, apparmor, etc) from the point of view of a capability.
type securitySystem interface {
	// TODO: change snap to appropriate type after current transition
	GrantPermissions(snapName string, cap *Capability) error
	// TODO: change snap to appropriate type after current transition
	RevokePermissions(snapName string, cap *Capability) error
}
