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

package configcore

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/snap/naming"
)

const vitalityOpt = "resilience.vitality-hint"

func init() {
	// add supported configuration of this module
	supportedConfigurations["core."+vitalityOpt] = true
}

func handleVitalityConfiguration(tr config.ConfGetter, opts *fsOnlyContext) error {
	// XXX: rewrite/restart changed configs

	return nil
}

func validateVitalitySettings(tr config.ConfGetter) error {
	option, err := coreCfg(tr, vitalityOpt)
	if err != nil {
		return err
	}
	if option == "" {
		return nil
	}
	vitalityHints := strings.Split(option, ",")
	if len(vitalityHints) > 100 {
		return fmt.Errorf("cannot set more than 100 %q values: got %v", vitalityOpt, len(vitalityHints))
	}
	for _, instanceName := range vitalityHints {
		if err := naming.ValidateInstance(instanceName); err != nil {
			return fmt.Errorf("cannot set %q: %v", vitalityOpt, err)
		}
	}

	return nil
}
