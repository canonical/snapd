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

package main

import (
	"os"

	"github.com/ubuntu-core/snappy/asserts"
	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
)

type assertsOptions struct {
	AssertTypeName string   `positional-arg-name:"assertion-type" description:"assertion type name" required:"true"`
	HeaderFilters  []string `positional-arg-name:"header-filters" description:"header=value" required:"false"`
}

type cmdAsserts struct {
	assertsOptions `positional-args:"true" required:"true"`
}

var (
	shortAssertsHelp = i18n.G("Asserts searches the system for assertions of the given type")
	longAssertsHelp  = i18n.G(`This command searches for assertions of the given type and matching the given header filters (header=value) in the system assertion database.`)
)

func init() {
	_, err := parser.AddCommand("asserts", shortAssertsHelp, longAssertsHelp, &cmdAsserts{})
	if err != nil {
		logger.Panicf("unable to add asserts command: %v", err)
	}
}

func (x *cmdAsserts) Execute(args []string) error {
	// XXX: use HeaderFilters

	assertions, err := client.New().Asserts(x.AssertTypeName, nil)
	if err != nil {
		return err
	}

	// XXX: special case len == 0

	enc := asserts.NewEncoder(os.Stdout)

	for _, a := range assertions {
		enc.Encode(a)
	}

	return nil
}
