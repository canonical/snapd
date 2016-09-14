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

package kernel_module

import (
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// Backend is responsible for maintaining kernel modules
type Backend struct{}

// Name returns the name of the backend.
func (b *Backend) Name() string {
	return "kernel_module"
}

func (b *Backend) Setup(snapInfo *snap.Info, devMode bool, repo *interfaces.Repository) error {
	// TODO: get snippets, load modules, create /etc/modules-load.d/snap.modules.conf file
	return nil
}

func (b *Backend) Remove(snapName string) error {
	return nil
}
