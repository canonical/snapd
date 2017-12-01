// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"time"

	"github.com/snapcore/snapd/osutil"
)

type UI interface {
	// YesNo asks a yes/no question. The primary text
	// will be printed in a larger font, the secondary text
	// in the standard font and the (optional) footer will
	// be printed in a small font.
	//
	// The value "true" is returned if the user clicks "yes",
	// other wise "false".
	YesNo(primary, secondary string, options *Options) bool
}

type Options struct {
	Footer  string
	Timeout time.Duration
}

func New() (UI, error) {
	switch {
	case osutil.ExecutableExists("zenity"):
		return &Zenity{}, nil
	default:
		return nil, fmt.Errorf("cannot create a suitable UI")
	}
}
