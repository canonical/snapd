// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

// UI is an interface for user interaction
type UI interface {
	// YesNo asks a yes/no question. The primary text
	// will be printed in a larger font, the secondary text
	// in the standard font and the (optional) footer will
	// be printed in a small font.
	//
	// The value "true" is returned if the user clicks "yes",
	// otherwise "false".
	YesNo(primary, secondary string, options *Options) bool
}

// Options for the UI interface
type Options struct {
	Footer string
	// Timeout in seconds. We do not use time.Duration because
	// this gets passed to zenity and that only supports seconds.
	Timeout int
}

// New returns the best matching UI interface for the given system
// or an error if no ui can be created.
func New() (UI, error) {
	switch {
	case osutil.ExecutableExists("zenity") && osutil.ExecutableExists("kdialog"):
		// prefer kdialog on KDE, otherwise use zenity
		currentDesktop := os.Getenv("XDG_CURRENT_DESKTOP")
		if strings.Contains("KDE", currentDesktop) || strings.Contains("kde", currentDesktop) {
			return &Kdialog{}, nil
		}
		return &Zenity{}, nil
	case osutil.ExecutableExists("zenity"):
		return &Zenity{}, nil
	case osutil.ExecutableExists("kdialog"):
		return &Kdialog{}, nil
	default:
		return nil, fmt.Errorf("cannot create a UI: please install zenity or kdialog")
	}
}
