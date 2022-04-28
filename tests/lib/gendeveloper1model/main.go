// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

// The ``gendevmodel'' tool can be used generate model assertions for use in
// tests. It reads the assetion headers in form of a JSON from stdin, and
// outputs a model assertion, signed by the test key to stdout.
//
// Usage:
//       gendeveloper1model < headers.json > assertion.model
//
// Example input:
//
// {
//     "type": "model",
//     "brand-id": "developer1",
//     "model": "my-model",
//     "architecture": "amd64",
//     "gadget": "test-snapd-pc",
//     "kernel": "pc-kernel=18",
//     "timestamp": "2018-09-11T22:00:00+00:00"
// }
//
package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

func main() {
	devKey, _ := assertstest.ReadPrivKey(assertstest.DevKey)
	devSigning := assertstest.NewSigningDB("developer1", devKey)

	var headers map[string]interface{}
	dec := json.NewDecoder(os.Stdin)
	if err := dec.Decode(&headers); err != nil {
		log.Fatalf("failed to decode model headers data: %v", err)
	}

	assertName, _ := headers["type"]
	assertType := asserts.ModelType
	if assertName == "system-user" {
		assertType = asserts.SystemUserType
	}

	clModel, err := devSigning.Sign(assertType, headers, nil, "")
	if err != nil {
		log.Fatalf("failed to sign the model: %v", err)
	}
	os.Stdout.Write(asserts.Encode(clModel))
}
