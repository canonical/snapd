// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"errors"
	"io/ioutil"

	"github.com/ubuntu-core/snappy/asserts"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
)

type cmdAddAssertion struct {
	Positional struct {
		AssertionFile string `positional-arg-name:"assertion file"`
	} `positional-args:"yes"`
}

func init() {
	arg, err := parser.AddCommand("add-assertion",
		i18n.G("Add an assertion to the assertion database"),
		i18n.G("Add an assertion to the assertion database"),
		&cmdAddAssertion{})
	if err != nil {
		logger.Panicf("Unable to install: %v", err)
	}
	addOptionDescription(arg, "assertion file", i18n.G("The file containing the assertion to add"))
}

func (x *cmdAddAssertion) Execute(args []string) error {
	// XXX: no locking atm
	return x.doAddAssertion()
}

func (x *cmdAddAssertion) doAddAssertion() error {
	assertFile := x.Positional.AssertionFile

	if assertFile == "" {
		return errors.New(i18n.G("assertion file is required"))
	}

	sysAssertDb, err := asserts.OpenSysDatabase()
	if err != nil {
		return err
	}

	assertData, err := ioutil.ReadFile(assertFile)
	if err != nil {
		return err
	}

	assert, err := asserts.Decode(assertData)
	if err != nil {
		return err
	}

	return sysAssertDb.Add(assert)
}
