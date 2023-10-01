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

package policy

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
)

const baseDeclarationHeader = `
type: base-declaration
authority-id: canonical
series: 16
revision: 0
`

const baseDeclarationPlugs = `
plugs:
`

const baseDeclarationSlots = `
slots:
`

func trimTrailingNewline(s string) string {
	return strings.TrimRight(s, "\n")
}

// composeBaseDeclaration composes the headers of the builtin base-declaration
// describing the default interface policies for all snaps from each interface
// StaticInfo.BaseDeclarationSlots/BaseDeclarationPlgs.
// See interfaces/builtin/README.md
func composeBaseDeclaration(ifaces []interfaces.Interface) ([]byte, error) {
	var buf bytes.Buffer
	// Trim newlines at the end of the string. All the elements may have
	// spurious trailing newlines. All elements start with a leading newline.
	// We don't want any blanks as that would no longer parse.
	if _, err := buf.WriteString(trimTrailingNewline(baseDeclarationHeader)); err != nil {
		return nil, err
	}
	if _, err := buf.WriteString(trimTrailingNewline(baseDeclarationPlugs)); err != nil {
		return nil, err
	}
	for _, iface := range ifaces {
		plugPolicy := interfaces.StaticInfoOf(iface).BaseDeclarationPlugs
		if _, err := buf.WriteString(trimTrailingNewline(plugPolicy)); err != nil {
			return nil, err
		}
	}
	if _, err := buf.WriteString(trimTrailingNewline(baseDeclarationSlots)); err != nil {
		return nil, err
	}
	for _, iface := range ifaces {
		slotPolicy := interfaces.StaticInfoOf(iface).BaseDeclarationSlots
		if _, err := buf.WriteString(trimTrailingNewline(slotPolicy)); err != nil {
			return nil, err
		}
	}
	if _, err := buf.WriteRune('\n'); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func init() {
	decl, err := composeBaseDeclaration(builtin.Interfaces())
	if err != nil {
		panic(fmt.Sprintf("cannot compose base-declaration: %v", err))
	}
	if err := asserts.InitBuiltinBaseDeclaration(decl); err != nil {
		panic(fmt.Sprintf("cannot initialize the builtin base-declaration: %v", err))
	}
}
