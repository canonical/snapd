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

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/tests/lib/fakestore/refresh"
)

type cmdNewSnapDeclaration struct {
	Positional struct {
		Snap string `description:"Snap file"`
	} `positional-args:"yes"`

	TopDir           string `long:"dir" description:"Directory to be used by the store to keep and serve snaps, <dir>/asserts is used for assertions"`
	SnapDeclJsonPath string `long:"snap-decl-json" description:"Path to a json encoded snap declaration"`
}

func (x *cmdNewSnapDeclaration) Execute(args []string) error {
	headers := map[string]interface{}{}
	if x.SnapDeclJsonPath != "" {
		content := mylog.Check2(os.ReadFile(x.SnapDeclJsonPath))
		mylog.Check(json.Unmarshal(content, &headers))

	}

	p := mylog.Check2(refresh.NewSnapDeclaration(x.TopDir, x.Positional.Snap, headers))

	fmt.Println(p)
	return nil
}

var shortNewSnapDeclarationHelp = "Make new snap declaration"

var longNewSnapDeclarationHelp = `
Generate snap declaration signed with test keys. By default
the snap name and snap ID are derived from the file name.
`

func init() {
	parser.AddCommand("new-snap-declaration", shortNewSnapDeclarationHelp, longNewSnapDeclarationHelp,
		&cmdNewSnapDeclaration{})
}
