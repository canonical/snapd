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
	"time"

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
	YesNo(primary, secondary string, options *DialogOptions) bool
}

// Options for the UI interface
type DialogOptions struct {
	Footer  string
	Timeout time.Duration
}

var hasZenityExecutable = func() bool {
	return osutil.ExecutableExists("zenity")
}

var hasKDialogExecutable = func() bool {
	return osutil.ExecutableExists("kdialog")
}

func MockHasZenityExecutable(f func() bool) func() {
	oldHasZenityExecutable := hasZenityExecutable
	hasZenityExecutable = f
	return func() {
		hasZenityExecutable = oldHasZenityExecutable
	}
}

func MockHasKDialogExecutable(f func() bool) func() {
	oldHasKDialogExecutable := hasKDialogExecutable
	hasKDialogExecutable = f
	return func() {
		hasKDialogExecutable = oldHasKDialogExecutable
	}
}

// New returns the best matching UI interface for the given system
// or an error if no ui can be created.
func New() (UI, error) {
	hasZenity := hasZenityExecutable()
	hasKDialog := hasKDialogExecutable()

	switch {
	case hasZenity && hasKDialog:
		// prefer kdialog on KDE, otherwise use zenity
		currentDesktop := os.Getenv("XDG_CURRENT_DESKTOP")
		if strings.Contains("kde", strings.ToLower(currentDesktop)) {
			return &KDialog{}, nil
		}
		return &Zenity{}, nil
	case hasZenity:
		return &Zenity{}, nil
	case hasKDialog:
		return &KDialog{}, nil
	default:
		return nil, fmt.Errorf("cannot create a UI: please install zenity or kdialog")
	}
}
