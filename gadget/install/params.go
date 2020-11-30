// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package install

import (
	"github.com/snapcore/snapd/secboot"
)

type Options struct {
	// Also mount the filesystems after creation
	Mount bool
	// Encrypt the data partition
	Encrypt bool
}

// EncryptionKeySet is a set of encryption keys.
type EncryptionKeySet struct {
	Key         secboot.EncryptionKey
	RecoveryKey secboot.RecoveryKey
}

// InstalledSystemSideData carries side data of an installed system, eg. secrets
// to access its partitions.
type InstalledSystemSideData struct {
	// KeysForRoles contains key sets for the relevant structure roles.
	KeysForRoles map[string]*EncryptionKeySet
}
