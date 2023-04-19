// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package main

import (
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/sysconfig"
)

func init() {
	const (
		short = "Configure the gadget"
		long  = "Configure the gadget"
	)

	addCommandBuilder(func(parser *flags.Parser) {
		if _, err := parser.AddCommand("configure-gadget", short, long, &cmdConfigureGadget{}); err != nil {
			panic(err)
		}
	})

	snap.SanitizePlugsSlots = func(*snap.Info) {}
}

type cmdConfigureGadget struct{}

func (c *cmdConfigureGadget) Execute([]string) error {
	return configureGadget()
}

func configureGadget() error {
	_, recoverySystem, err := boot.ModeAndRecoverySystemFromKernelCommandLine()
	if err != nil {
		return err
	}

	typs := []snap.Type{snap.TypeBase, snap.TypeKernel, snap.TypeSnapd, snap.TypeGadget}
	model, essSnaps, err := readEssential(recoverySystem, typs)
	if err != nil {
		return err
	}

	gadgetPath := ""
	for _, essentialSnap := range essSnaps {
		if essentialSnap.EssentialType == snap.TypeGadget {
			gadgetPath = essentialSnap.Path
		}
	}
	gadgetSnap := squashfs.New(gadgetPath)

	isRunMode := false
	// we need to configure the ephemeral system with defaults and such using
	// from the seed gadget
	configOpts := &sysconfig.Options{
		// never allow cloud-init to run inside the ephemeral system, in the
		// install case we don't want it to ever run, and in the recover case
		// cloud-init will already have run in run mode, so things like network
		// config and users should already be setup and we will copy those
		// further down in the setup for recover mode
		AllowCloudInit: false,
		TargetRootDir:  boot.InitramfsWritableDir(model, isRunMode),
		GadgetSnap:     gadgetSnap,
	}
	if err := sysconfig.ConfigureTargetSystem(model, configOpts); err != nil {
		return err
	}

	return nil
}
