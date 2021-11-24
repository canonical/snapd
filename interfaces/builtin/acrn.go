// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package builtin

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/snap"
)

const acrnSummary = `allows access to the acrn_hsm device`

const acrnBaseDeclarationSlots = `
  acrn:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const acrnConnectedPlugAppArmor = `
# Description: Allow write access to acrn_hsm.
/dev/acrn_hsm rw,
# Allow offlining CPU cores
/sys/devices/system/cpu/cpu[0-9]*/online rw,

`

type acrnInterface struct {
	commonInterface
}

var acrnConnectedPlugUDev = []string{
    `KERNEL=="acrn_hsm"`,
}

func init() {
	registerIface(&acrnInterface{commonInterface{
		name:                  "acrn",
		summary:               acrnSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
        connectedPlugUDev:     acrnConnectedPlugUDev,
		baseDeclarationSlots:  acrnBaseDeclarationSlots,
		connectedPlugAppArmor: acrnConnectedPlugAppArmor,
	}})
}
