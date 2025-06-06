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

// The “gendeveloper1” tool can be used generate model assertions for use in
// tests. It reads the assetion headers in form of a JSON from stdin, and
// outputs a model assertion, signed by the test key to stdout.
//
// Usage:
//
//	gendeveloper1 sign-model < headers.json > assertion.model
//
// Example input:
//
//	{
//	    "type": "model",
//	    "brand-id": "developer1",
//	    "model": "my-model",
//	    "architecture": "amd64",
//	    "gadget": "test-snapd-pc",
//	    "kernel": "pc-kernel=18",
//	    "timestamp": "2018-09-11T22:00:00+00:00"
//	}
//
// --root-key can be used with any of the commands to use the testrootorg key instead.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/systestkeys"
)

type cmdShowKey struct {
	RootKey bool `long:"root-key" description:"show the test root key instead of the developer key"`
}

// ShowKey is a command that prints a key to stdout.
func (c *cmdShowKey) Execute(args []string) error {
	key := assertstest.DevKey
	if c.RootKey {
		key = systestkeys.TestRootPrivKey
	}

	fmt.Printf("%s", key)
	return nil
}

type cmdSignModel struct {
	RootKey bool `long:"root-key" description:"use the test root key instead of the developer key for signing"`
}

// SignModel is a command that signs a model assertion based on the headers.
func (c *cmdSignModel) Execute(args []string) error {
	var devKey asserts.PrivateKey
	var devSigning *assertstest.SigningDB
	if c.RootKey {
		devKey, _ = assertstest.ReadPrivKey(systestkeys.TestRootPrivKey)
		devSigning = assertstest.NewSigningDB("testrootorg", devKey)
	} else {
		devKey, _ = assertstest.ReadPrivKey(assertstest.DevKey)
		devSigning = assertstest.NewSigningDB("developer1", devKey)
	}

	var headers map[string]interface{}
	dec := json.NewDecoder(os.Stdin)
	if err := dec.Decode(&headers); err != nil {
		log.Fatalf("failed to decode model headers data: %v", err)
	}

	headerType := headers["type"]
	assertType := asserts.ModelType
	if assertTypeStr, ok := headerType.(string); ok {
		assertType = asserts.Type(assertTypeStr)
	}

	var body []byte
	if bodyHeader, ok := headers["body"]; ok {
		bodyStr, ok := bodyHeader.(string)
		if !ok {
			log.Fatalf("failed to decode body: expected string but got %T", bodyHeader)
		}
		body = []byte(bodyStr)
		delete(headers, "body")
	}

	clModel, err := devSigning.Sign(assertType, headers, body, "")
	if err != nil {
		log.Fatalf("failed to sign the model: %v", err)
	}
	os.Stdout.Write(asserts.Encode(clModel))

	return nil
}

type Options struct {
	ShowKey   cmdShowKey   `command:"show-key" description:"Print the developer key to stdout"`
	SignModel cmdSignModel `command:"sign-model" description:"Sign a model assertion"`
}

var options Options

var parser = flags.NewParser(&options, flags.Default)

func main() {
	if _, err := parser.Parse(); err != nil {
		switch flagsErr := err.(type) {
		case flags.ErrorType:
			if flagsErr == flags.ErrHelp {
				os.Exit(0)
			}
			os.Exit(1)
		default:
			os.Exit(1)
		}
	}
}
