// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

	"github.com/ubuntu-core/snappy/classic"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
)

type cmdEnableClassic struct {
}

var shortEnableClassicHelp = i18n.G("Enable classic dimension.")

var longEnableClassicHelp = i18n.G("Enable the ubuntu classic dimension.")

func init() {
	_, err := parser.AddCommand("enable-classic",
		shortEnableClassicHelp,
		longEnableClassicHelp,
		&cmdEnableClassic{})
	if err != nil {
		logger.Panicf("Unable to enable-classic: %v", err)
	}
}

func (x *cmdEnableClassic) Execute(args []string) (err error) {
	if classic.Enabled() {
		return fmt.Errorf(i18n.G("Classic dimension is already enabled."))
	}

	if err := classic.Create(); err != nil {
		return err
	}

	fmt.Println(`Classic dimension enabled on this snappy system.
Use “sudo snappy shell classic” to enter the classic dimension.`)
	return nil
}
