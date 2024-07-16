// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2019 Canonical Ltd
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

package seedtest

var SampleSnapYaml = map[string]string{
	"core": `name: core
type: os
version: 1.0
`,
	"pc-kernel": `name: pc-kernel
type: kernel
version: 1.0
`,
	"pc": `name: pc
type: gadget
version: 1.0
`,
	"classic-gadget": `name: classic-gadget
version: 1.0
type: gadget
`,
	"required": `name: required
type: app
version: 1.0
`,
	"classic-snap": `name: classic-snap
type: app
confinement: classic
version: 1.0
`,
	"snapd": `name: snapd
type: snapd
version: 1.0
`,
	"core18": `name: core18
type: base
version: 1.0
`,
	"pc-kernel=18": `name: pc-kernel
type: kernel
version: 1.0
`,
	"pc=18": `name: pc
type: gadget
base: core18
version: 1.0
`,
	"classic-gadget18": `name: classic-gadget18
version: 1.0
base: core18
type: gadget
`,
	"required18": `name: required18
type: app
base: core18
version: 1.0
`,
	"core20": `name: core20
type: base
version: 1.0
`,
	"pc-kernel=20": `name: pc-kernel
type: kernel
version: 1.0
`,
	"pc=20": `name: pc
type: gadget
base: core20
version: 1.0
`,
	"required20": `name: required20
type: app
base: core20
version: 1.0
components:
  comp1:
    type: test
  comp2:
    type: test
`,
	"required20+comp1": `component: required20+comp1
type: test
version: 1.0
`,
	"required20+comp1_kernel": `component: required20+comp1
type: kernel-modules
version: 1.0
`,
	"required20+comp2": `component: required20+comp2
type: test
version: 2.0
`,
	"required20+unknown": `component: required20+unknown
type: test
version: 2.0
`,
	"optional20-a": `name: optional20-a
type: app
base: core20
version: 1.0
`,
	"optional20-b": `name: optional20-b
type: app
base: core20
version: 1.0`,
	"uboot-gadget=20": `name: uboot-gadget
type: gadget
base: core20
version: 1.0
`,
	"arm-kernel=20": `name: arm-kernel
type: kernel
version: 1.0
`,
	"test-devmode=20": `name: test-devmode
type: app
base: core20
version: 1.0
confinement: devmode
`,
	"core22": `name: core22
type: base
version: 1.0
`,
	"pc-kernel=22": `name: pc-kernel
type: kernel
version: 1.0
`,
	"pc=22": `name: pc
type: gadget
base: core22
version: 1.0
`,
}

func MergeSampleSnapYaml(snapYaml ...map[string]string) map[string]string {
	if len(snapYaml) == 0 {
		return nil
	}
	merged := make(map[string]string, len(snapYaml[0]))
	for _, m := range snapYaml {
		for yamlKey, yaml := range m {
			merged[yamlKey] = yaml
		}
	}
	return merged
}
