// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package caps

import (
	"regexp"
)

// BoolFileType is a built-in capability type for files that follow a
// simple boolean protocol. The file can be read, which yields ASCII '0'
// (zero) or ASCII '1' (one). The same can be done for writing.
//
// This capability type can be used to describe many boolean flags exposed
// in sysfs, including certain hardware like exported GPIO pins.
var BoolFileType = &Type{
	Name: "bool-file",
	Attrs: map[string]TypeAttr{
		"path": &pathAttr{
			errorHint: "only LED brightness or GPIO value is allowed",
			// Allowed devices include:
			allowedPatterns: []*regexp.Regexp{
				// The brightness of standard LED class device
				regexp.MustCompile("^/sys/class/leds/[^/]+/brightness$"),
				// The value of standard exported GPIO
				regexp.MustCompile("^/sys/class/gpio/gpio[0-9]+/value$"),
			},
		},
	},
}
