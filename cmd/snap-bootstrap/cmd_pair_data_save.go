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
	"fmt"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/snap"
)

func init() {
	const (
		short = "Compare data and save mounts"
		long  = "Compare data and save mounts"
	)

	addCommandBuilder(func(parser *flags.Parser) {
		if _, err := parser.AddCommand("compare-data-save", short, long, &cmdCompareDataSave{}); err != nil {
			panic(err)
		}
	})

	snap.SanitizePlugsSlots = func(*snap.Info) {}
}

type cmdCompareDataSave struct{}

func (c *cmdCompareDataSave) Execute([]string) error {
	return compareDataSave()
}

func compareDataSave() error {
	// FIXME: this is only valid for run mode
	model, err := getUnverifiedBootModel()
	if err != nil {
		return err
	}
	rootDir := boot.InitramfsWritableDir(model, true)
	paired, err := checkDataAndSavePairing(rootDir)
	if err != nil {
		return err
	}
	if !paired {
		return fmt.Errorf("cannot validate boot: ubuntu-save and ubuntu-data are not marked as from the same install")
	}
	return nil

}
