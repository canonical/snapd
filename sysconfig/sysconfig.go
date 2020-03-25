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

package sysconfig

type Options struct {
	// TODO: do we really want this kind of specific dir pointers
	// or more general ones?
	CloudInitSrcDir string
}

// ConfigureRunSystem configures the ubuntu-data partition with any
// configuration needed from e.g. the gadget or for cloud-init.
func ConfigureRunSystem(opts *Options) error {
	if err := configureCloudInit(opts); err != nil {
		return err
	}

	return nil
}
