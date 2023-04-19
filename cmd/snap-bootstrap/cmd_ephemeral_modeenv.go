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
)

func init() {
	const (
		short = "Generate an ephemeral modeenv"
		long  = "Generate an ephemeral modeenv"
	)

	addCommandBuilder(func(parser *flags.Parser) {
		if _, err := parser.AddCommand("ephemeral-modeenv", short, long, &cmdEphemeralModeenv{}); err != nil {
			panic(err)
		}
	})

	snap.SanitizePlugsSlots = func(*snap.Info) {}
}

type cmdEphemeralModeenv struct{}

func (c *cmdEphemeralModeenv) Execute([]string) error {
	return generateEphemeralModeenv()
}

func generateEphemeralModeenv() error {
	mode, recoverySystem, err := boot.ModeAndRecoverySystemFromKernelCommandLine()
	if err != nil {
		return err
	}

	typs := []snap.Type{snap.TypeBase, snap.TypeKernel, snap.TypeSnapd, snap.TypeGadget}
	model, essSnaps, err := readEssential(recoverySystem, typs)
	if err != nil {
		return err
	}

	systemSnaps := make(map[snap.Type]snap.PlaceInfo)

	for _, essentialSnap := range essSnaps {
		systemSnaps[essentialSnap.EssentialType] = essentialSnap.PlaceInfo()
	}

	modeEnv := &boot.Modeenv{
		Mode:           mode,
		RecoverySystem: recoverySystem,
		Base:           systemSnaps[snap.TypeBase].Filename(),
		Gadget:         systemSnaps[snap.TypeGadget].Filename(),
		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
	}

	isRunMode := false
	if err := modeEnv.WriteTo(boot.InitramfsWritableDir(model, isRunMode)); err != nil {
		return err
	}

	return nil
}
