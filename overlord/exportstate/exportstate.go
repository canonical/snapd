// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

// Package exportstate implements the manager and state aspects responsible
// for the exporting portions of installed snaps to the system.
package exportstate

// TODO: add export manager, task hooks and glue connecting it to the overlord.

// TODO: on initialization of export manager, elect the new provider of snapd
// tools, so that it is guaranteed to be present before we need to invoke
// anything in snap world or before we need to setup security profiles.

// TODO: track export sets in the state, so that for each on-disk tuple
// (PrimaryKey, SubKey, ExportSetName) we can find the associated snap name and
// snap revision. This may not be required for snapd but might be required in
// the general case, where snaps provide possibly-conflicting exports and snapd
// picks one or another snap as a provider (e.g. manual page de-conflicting).

// TODO: add function electing the new provider of an export set, so that the
// export-set current symlink can be updated.

// TODO: add a special-case function that elects new provider of snapd tools,
// which span two snaps and the classic host.

// TODO: add function that performs the mechanics of switching the current
// provider of an export set, performing appropriate locking or using lockless
// primitives where available.
