// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"strings"

	"github.com/jessevdk/go-flags"
)

type cmdDebugFeatures struct {
	clientMixin

	p *flags.Parser
}

func init() {
	addDebugCommand("features",
		"Obtain the complete list of feature tags",
		`Display json output that contains the complete list 
		of feature tags present in snapd and snap`,
		func() flags.Commander { return &cmdDebugFeatures{} },
		nil,
		nil,
	)
}

func (x *cmdDebugFeatures) setParser(p *flags.Parser) {
	x.p = p
}

func (x *cmdDebugFeatures) Execute(args []string) error {
	x.setClient(mkClient())
	var err error
	var rsp map[string]any
	err = x.client.DebugGet("features", &rsp, nil)
	if err != nil {
		return err
	}
	rsp["commands"] = getCommandNames(x.p)
	enc := json.NewEncoder(Stdout)
	if err := enc.Encode(rsp); err != nil {
		return err
	}
	return nil
}

func getCommandNames(parser *flags.Parser) []string {
	commands := parser.Command.Commands()
	names := []string{}
	for _, cmd := range commands {
		subcommands := cmd.Commands()
		if len(subcommands) == 0 {
			names = append(names, cmd.Name)
		} else {
			names = append(names, getSubCommandNames(subcommands, []string{cmd.Name})...)
		}
	}
	return names
}

func getSubCommandNames(commands []*flags.Command, names []string) []string {
	composedNames := []string{}
	for _, cmd := range commands {
		subcommands := cmd.Commands()
		if len(subcommands) == 0 {
			composedNames = append(composedNames, strings.Join(append(names, cmd.Name), " "))
		} else {
			composedNames = append(composedNames, getSubCommandNames(subcommands, append(names, cmd.Name))...)
		}
	}
	return composedNames
}
