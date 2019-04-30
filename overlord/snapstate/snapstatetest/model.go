// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package snapstatetest

import (
	"github.com/snapcore/snapd/asserts"
)

func MakeModel(override map[string]string) *asserts.Model {
	model := map[string]interface{}{
		"type":              "model",
		"authority-id":      "brand",
		"series":            "16",
		"brand-id":          "brand",
		"model":             "baz-3000",
		"architecture":      "armhf",
		"gadget":            "brand-gadget",
		"kernel":            "kernel",
		"timestamp":         "2018-01-01T08:00:00+00:00",
		"sign-key-sha3-384": "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
	}
	for k, v := range override {
		model[k] = v
	}

	a, err := asserts.Assemble(model, nil, nil, []byte("AXNpZw=="))
	if err != nil {
		panic(err)
	}

	return a.(*asserts.Model)
}
