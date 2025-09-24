// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2025 Canonical Ltd
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

	"github.com/snapcore/snapd/interfaces/builtin"
)

func isValidInterface(name string) bool {
	for _, ifr := range builtin.Interfaces() {
		if ifr.Name() == name {
			return true
		}
	}
	return false
}

func isValidInterfaceOption(opt string) bool {
	switch opt {
	case "allow-auto-connection":
		return true
	}
	return false
}

func isInterfaceChange(opt string) bool {
	return strings.HasPrefix(opt, "core.interface.")
}

func validateInterfaceChange(opt string) error {
	// core.interface.<interface>.<option>
	tokens := strings.SplitN(opt, ".", 4)
	if tokens[0] != "core" && tokens[1] != "interface" {
		return fmt.Errorf("unsupported or wrongly formatted interface change: %s", opt)
	}
	if !isValidInterface(tokens[2]) {
		return fmt.Errorf("unsupported interface %q for configuration change", tokens[2])
	}
	if !isValidInterfaceOption(tokens[3]) {
		return fmt.Errorf("unsupported interface option: %q", tokens[3])
	}
	return nil
}

func validateAllowAutoConnectionValue(tr RunTransaction) error {
	for _, name := range tr.Changes() {
		if !strings.HasPrefix(name, "core.interface.") {
			continue
		}
		if !strings.HasSuffix(name, ".allow-auto-connection") {
			continue
		}

		nameWithoutSnap := strings.SplitN(name, ".", 2)[1]
		value, err := coreCfg(tr, nameWithoutSnap)
		if err != nil {
			return fmt.Errorf("internal error: cannot get data for %s: %v", name, err)
		}

		switch value {
		case "", "verified", "true", "false":
			// thats ok
		default:
			return fmt.Errorf("%s can only be set to 'true', 'false' or 'verified'", name)
		}
	}
	return nil
}
