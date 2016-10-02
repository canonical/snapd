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

package builtin

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
)

// the headers of the builtin base-declaration describing the default
// interface policies for all snaps.
const baseDeclarationHeaders = `
type: base-declaration
authority-id: canonical
series: 16
revision: 0
plugs:
  network: true
slots:
  network:
    allow-installation:
      slot-snap-type:
        - core
`

func init() {
	err := asserts.InitBuiltinBaseDeclaration([]byte(baseDeclarationHeaders))
	if err != nil {
		panic(fmt.Sprintf("cannot initialize the builtin base-declaration: %v", err))
	}
}
