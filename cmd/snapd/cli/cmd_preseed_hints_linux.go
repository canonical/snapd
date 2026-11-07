// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package cli

import (
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/image/preseed"
)

type cmdPreseedHints struct {
	Positional struct {
		HintsFile string `positional-arg-name:"<hints-file>"`
	} `positional-args:"yes" required:"yes"`
}

var shortPreseedHintsHelp = i18n.G("Write preseeding hints about this device")
var longPreseedHintsHelp = i18n.G(`
The preseed-hints command captures the properties of this device that are
needed to preseed an image for it, and writes them to the given file as a
versioned hints document.

The hints file can then be copied to a build host and passed to 'snap
prepare-image --preseed --hints <hints-file>', which reconstructs what it
describes for the duration of the preseeding steps. This makes it possible to
preseed an image on a build host whose hardware differs from the target device.

The hints currently describe the /sys/class entries (such as /sys/class/gpio,
/sys/class/leds, /sys/class/pwm) that snap interface security profile
generation looks at, together with the /sys/devices paths backing them.

If the hints file already exists the command exits with an error rather than
overwriting it.`)

func init() {
	addCommand("preseed-hints",
		shortPreseedHintsHelp,
		longPreseedHintsHelp,
		func() flags.Commander { return &cmdPreseedHints{} },
		nil,
		[]argDesc{
			{
				// TRANSLATORS: This needs to begin with < and end with >
				name: i18n.G("<hints-file>"),
				// TRANSLATORS: This should not start with a lowercase letter.
				desc: i18n.G("Path of the preseed hints file to write"),
			},
		})
}

var preseedWritePreseedHints = preseed.WritePreseedHints

func (x *cmdPreseedHints) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	return preseedWritePreseedHints(x.Positional.HintsFile)
}
