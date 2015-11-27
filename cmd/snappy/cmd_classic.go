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
	"github.com/ubuntu-core/snappy/progress"
)

type cmdEnableClassic struct{}
type cmdDestroyClassic struct{}

func init() {
	_, err := parser.AddCommand("enable-classic",
		i18n.G("Enable classic dimension."),
		i18n.G("Enable the ubuntu classic dimension."),
		&cmdEnableClassic{})
	if err != nil {
		logger.Panicf("Unable to enable-classic: %v", err)
	}

	_, err = parser.AddCommand("destroy-classic",
		i18n.G("Destroy the classic dimension."),
		i18n.G("Destroy the ubuntu classic dimension."),
		&cmdDestroyClassic{})
	if err != nil {
		logger.Panicf("Unable to destroy-classic: %v", err)
	}
}

func (x *cmdEnableClassic) Execute(args []string) (err error) {
	if classic.Enabled() {
		return fmt.Errorf(i18n.G("Classic dimension is already enabled."))
	}

	pbar := progress.NewTextProgress()
	if err := classic.Create(pbar); err != nil {
		return err
	}

	fmt.Println(i18n.G(`Classic dimension enabled on this snappy system.
Use “sudo snappy shell classic” to enter the classic dimension.`))
	return nil
}

func (x *cmdDestroyClassic) Execute(args []string) (err error) {
	if !classic.Enabled() {
		return fmt.Errorf(i18n.G("Classic dimension is not enabled."))
	}

	if err := classic.Destroy(); err != nil {
		return err
	}

	fmt.Println(i18n.G(`Classic dimension destroyed on this snappy system.`))
	return nil
}
