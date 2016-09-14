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

// Package selinux implements integration between snapd and snap-confine around
// selinux.
//
// NOTE: This is currently in very early stages. Expect things to fail and to
// be insecure.
package selinux

import (
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// Backend is responsible for maintaining apparmor profiles for ubuntu-core-launcher.
type Backend struct{}

// Name returns the name of the backend.
func (b *Backend) Name() string {
	return "selinux"
}

// Setup does nothing yet.
func (b *Backend) Setup(snapInfo *snap.Info, devMode bool, repo *interfaces.Repository) error {
	return nil
}

// Remove does nothing yet.
func (b *Backend) Remove(snapName string) error {
	return nil
}
