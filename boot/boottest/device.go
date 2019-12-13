// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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

package boottest

import (
	"strings"
)

// MockDevice implements boot.Device. It wraps a string like
// <boot-snap-name>[@<mode>], no <boot-snap-name> means classic, no
// <mode> defaults to "run".
type MockDevice string

func (d MockDevice) snapAndMode() []string {
	parts := strings.SplitN(string(d), "@", 2)
	if len(parts) == 1 {
		return append(parts, "run")
	}
	if parts[1] == "" {
		return []string{parts[0], "run"}
	}
	return parts
}

func (d MockDevice) Kernel() string { return d.snapAndMode()[0] }
func (d MockDevice) Base() string   { return d.snapAndMode()[0] }
func (d MockDevice) Classic() bool  { return d.snapAndMode()[0] == "" }
func (d MockDevice) RunMode() bool  { return d.snapAndMode()[1] == "run" }
