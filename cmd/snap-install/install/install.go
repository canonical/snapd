// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"github.com/snapcore/snapd/cmd/snap-install/volmgr"
)

type Options struct {
	Encrypt bool // use encrypted writable
}

type Install struct {
	gadgetRoot string
	device     string
	options    *Options
}

func New(gadgetRoot, device string, options *Options) *Install {
	return &Install{
		gadgetRoot: gadgetRoot,
		device:     device,
		options:    options,
	}
}

func (inst *Install) Run() error {
	v, err := volmgr.NewVolumeManager(inst.gadgetRoot, inst.device)
	if err != nil {
		return err
	}
	if err := v.Run(); err != nil {
		return err
	}

	return nil
}
