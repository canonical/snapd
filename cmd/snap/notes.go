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

package main

import (
	"strings"

	"github.com/snapcore/snapd/i18n"
)

// Notes encapsulate everything that might be interesting about a
// snap, in order to present a brief summary of it.
type Notes struct {
	Price    string
	Private  bool
	DevMode  bool
	JailMode bool
	TryMode  bool
	Disabled bool
	Broken   bool
}

func (n *Notes) String() string {
	var ns []string

	if n.Price != "" {
		ns = append(ns, n.Price)
	}

	if n.DevMode {
		ns = append(ns, "devmode")
	}

	if n.JailMode {
		ns = append(ns, "jailmode")
	}

	if n.Private {
		// TRANSLATORS: if possible, a single short word
		ns = append(ns, i18n.G("private"))
	}

	if n.TryMode {
		ns = append(ns, "try")
	}

	if n.Disabled {
		// TRANSLATORS: if possible, a single short word
		ns = append(ns, i18n.G("disabled"))
	}

	if n.Broken {
		// TRANSLATORS: if possible, a single short word
		ns = append(ns, i18n.G("broken"))
	}

	if len(ns) == 0 {
		return "-"
	}

	return strings.Join(ns, ",")
}
