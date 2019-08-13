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

package snap

import (
	"fmt"

	"github.com/snapcore/snapd/gadget"
)

// ReadGadgetInfo reads the gadget specific metadata from gadget.yaml
// in the snap. classic set to true means classic rules apply,
// i.e. content/presence of gadget.yaml is fully optional.
func ReadGadgetInfo(info *Info, classic bool) (*gadget.Info, error) {
	const errorFormat = "cannot read gadget snap details: %s"

	if info.GetType() != TypeGadget {
		return nil, fmt.Errorf(errorFormat, "not a gadget snap")
	}

	gi, err := gadget.ReadInfo(info.MountDir(), classic)
	if err != nil {
		return nil, fmt.Errorf(errorFormat, err)
	}
	return gi, nil
}
