// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"fmt"
	"sort"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
)

var shortSandboxFeaturesHelp = i18n.G("Print sandbox features available on the system")
var longSandboxFeaturesHelp = i18n.G(`
The sandbox command prints tags describing features of individual sandbox
components used by snapd on a given system.
`)

type cmdSandboxFeatures struct {
	Required []string `long:"required" arg-name:"<backend feature>"`
}

func init() {
	addDebugCommand("sandbox-features", shortSandboxFeaturesHelp, longSandboxFeaturesHelp, func() flags.Commander {
		return &cmdSandboxFeatures{}
	}, map[string]string{
		"required": i18n.G("Ensure that given backend:feature is available"),
	}, nil)
}

func (cmd cmdSandboxFeatures) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	cli := Client()
	sysInfo, err := cli.SysInfo()
	if err != nil {
		return err
	}

	sandboxFeatures := sysInfo.SandboxFeatures

	if len(cmd.Required) > 0 {
		avail := make(map[string]bool)
		for backend := range sandboxFeatures {
			for _, feature := range sandboxFeatures[backend] {
				avail[fmt.Sprintf("%s:%s", backend, feature)] = true
			}
		}
		for _, required := range cmd.Required {
			if !avail[required] {
				return fmt.Errorf("sandbox feature not available: %q", required)
			}
		}
	} else {
		backends := make([]string, 0, len(sandboxFeatures))
		for backend := range sandboxFeatures {
			backends = append(backends, backend)
		}
		sort.Strings(backends)
		w := tabWriter()
		defer w.Flush()
		for _, backend := range backends {
			fmt.Fprintf(w, "%s:\t%s\n", backend, strings.Join(sandboxFeatures[backend], " "))
		}
	}
	return nil
}
