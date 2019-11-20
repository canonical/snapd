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

package boot

import (
	"fmt"
	"io/ioutil"

	"github.com/mvo5/goconfigparser"

	"github.com/snapcore/snapd/dirs"
)

// Modeenv is a file on UC20 that provides additional information
// about the current mode (run,recover,install)
type Modeenv struct {
	Mode                string
	RecoverySystemLabel string
}

func ReadModeenv() (*Modeenv, error) {
	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	if err := cfg.ReadFile(dirs.SnapModeenvFile); err != nil {
		return nil, err
	}
	recoverySystemLabel, _ := cfg.Get("", "recovery_system")
	mode, _ := cfg.Get("", "mode")
	return &Modeenv{
		Mode:                mode,
		RecoverySystemLabel: recoverySystemLabel,
	}, nil
}

func WriteModeenv(m *Modeenv) error {
	// XXX: goconfigparser currently doesn't offer write functionality
	data := fmt.Sprintf("mode=%s\nrecovery_system=%s\nmode=%s\n", m.Mode, m.RecoverySystemLabel)
	return ioutil.WriteFile(dirs.SnapModeenvFile, []byte(data), 0644)
}
