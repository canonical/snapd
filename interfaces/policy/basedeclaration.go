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

	"github.com/ddkwork/golibrary/mylog"
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
	mylog.Check2(
		// Trim newlines at the end of the string. All the elements may have
		// spurious trailing newlines. All elements start with a leading newline.
		// We don't want any blanks as that would no longer parse.
		buf.WriteString(trimTrailingNewline(baseDeclarationHeader)))
	mylog.Check2(buf.WriteString(trimTrailingNewline(baseDeclarationPlugs)))

	for _, iface := range ifaces {
		plugPolicy := interfaces.StaticInfoOf(iface).BaseDeclarationPlugs
		mylog.Check2(buf.WriteString(trimTrailingNewline(plugPolicy)))

	}
	mylog.Check2(buf.WriteString(trimTrailingNewline(baseDeclarationSlots)))

	for _, iface := range ifaces {
		slotPolicy := interfaces.StaticInfoOf(iface).BaseDeclarationSlots
		mylog.Check2(buf.WriteString(trimTrailingNewline(slotPolicy)))

	}
	mylog.Check2(buf.WriteRune('\n'))

	return buf.Bytes(), nil
}

func init() {
	decl := mylog.Check2(composeBaseDeclaration(builtin.Interfaces()))
	mylog.Check(asserts.InitBuiltinBaseDeclaration(decl))
}
