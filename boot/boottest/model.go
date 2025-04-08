// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package boottest

import (
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

func MakeMockModel(overrides ...map[string]any) *asserts.Model {
	headers := map[string]any{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"display-name": "My Model",
		"architecture": "amd64",
		"base":         "core18",
		"gadget":       "pc=18",
		"kernel":       "pc-kernel=18",
		"timestamp":    "2018-01-01T08:00:00+00:00",
	}
	return assertstest.FakeAssertion(append([]map[string]any{headers}, overrides...)...).(*asserts.Model)
}

func MakeMockUC20Model(overrides ...map[string]any) *asserts.Model {
	headers := map[string]any{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model-uc20",
		"display-name": "My Model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"timestamp":    "2019-11-01T08:00:00+00:00",
		"snaps": []any{
			map[string]any{
				"name": "pc-kernel",
				"id":   "pckernelidididididididididididid",
				"type": "kernel",
			},
			map[string]any{
				"name": "pc",
				"id":   "pcididididididididididididididid",
				"type": "gadget",
			},
		},
	}
	return assertstest.FakeAssertion(append([]map[string]any{headers}, overrides...)...).(*asserts.Model)
}

func MakeMockClassicWithModesModel(overrides ...map[string]any) *asserts.Model {
	headers := map[string]any{
		"type":         "model",
		"classic":      "true",
		"distribution": "ubuntu",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model-classic-modes",
		"display-name": "My Model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"timestamp":    "2019-11-01T08:00:00+00:00",
		"snaps": []any{
			map[string]any{
				"name": "pc-kernel",
				"id":   "pckernelidididididididididididid",
				"type": "kernel",
			},
			map[string]any{
				"name": "pc",
				"id":   "pcididididididididididididididid",
				"type": "gadget",
			},
		},
	}
	return assertstest.FakeAssertion(append([]map[string]any{headers}, overrides...)...).(*asserts.Model)
}
