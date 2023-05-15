// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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

package snapstate_test

import (
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/snap/snaptest"
)

func ModelWithBase(baseName string) *asserts.Model {
	return MakeModel(map[string]interface{}{"base": baseName})
}

func ModelWithKernelTrack(kernelTrack string) *asserts.Model {
	return MakeModel(map[string]interface{}{"kernel": "kernel=" + kernelTrack})
}

func ModelWithGadgetTrack(gadgetTrack string) *asserts.Model {
	return MakeModel(map[string]interface{}{"gadget": "brand-gadget=" + gadgetTrack})
}

func DefaultModel() *asserts.Model {
	return MakeModel(nil)
}

func MakeModel(override map[string]interface{}) *asserts.Model {
	model := map[string]interface{}{
		"type":         "model",
		"authority-id": "brand",
		"series":       "16",
		"brand-id":     "brand",
		"model":        "baz-3000",
		"architecture": "armhf",
		"gadget":       "brand-gadget",
		"kernel":       "kernel",
		"timestamp":    "2018-01-01T08:00:00+00:00",
	}
	return assertstest.FakeAssertion(model, override).(*asserts.Model)
}

func MakeModel20(gadgetName string, override map[string]interface{}) *asserts.Model {
	model := map[string]interface{}{
		"type":         "model",
		"authority-id": "brand",
		"series":       "16",
		"brand-id":     "brand",
		"model":        "baz-3000",
		"architecture": "armhf",
		"base":         "core20",
		"grade":        "dangerous",
		"timestamp":    "2018-01-01T08:00:00+00:00",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "kernel",
				"id":              "kerneldididididididididididididi",
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            gadgetName,
				"id":              snaptest.AssertedSnapID(gadgetName),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	}
	return assertstest.FakeAssertion(model, override).(*asserts.Model)
}

func MakeModelClassicWithModes(gadgetName string, override map[string]interface{}) *asserts.Model {
	model := map[string]interface{}{
		"type":         "model",
		"authority-id": "brand",
		"series":       "16",
		"brand-id":     "brand",
		"model":        "baz-3000",
		"architecture": "amd64",
		"classic":      "true",
		"distribution": "ubuntu",
		"base":         "core22",
		"snaps": []interface{}{
			map[string]interface{}{
				"name": "kernel",
				"id":   "pclinuxdidididididididididididid",
				"type": "kernel",
			},
			map[string]interface{}{
				"name": gadgetName,
				"id":   "pcididididididididididididididid",
				"type": "gadget",
			},
		},
	}
	return assertstest.FakeAssertion(model, override).(*asserts.Model)
}

func ClassicModel() *asserts.Model {
	headers := map[string]interface{}{
		"type":         "model",
		"authority-id": "brand",
		"series":       "16",
		"brand-id":     "brand",
		"model":        "classicbaz-3000",
		"classic":      "true",
		"timestamp":    "2018-01-01T08:00:00+00:00",
	}
	return assertstest.FakeAssertion(headers).(*asserts.Model)
}
