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

	//"github.com/jessevdk/go-flags"

	"github.com/ubuntu-core/snappy/classic"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/progress"
)

// FIXME: Implement feature via "snap install classic"

type cmdEnableClassic struct{}
type cmdDestroyClassic struct{}

// FIXME: reenable for GA
/*
func init() {
	addCommand("enable-classic",
		i18n.G("Enable classic dimension."),
		i18n.G("Enable the ubuntu classic dimension."),
		func() flags.Commander {
			return &cmdEnableClassic{}
		})

	addCommand("destroy-classic",
		i18n.G("Destroy the classic dimension."),
		i18n.G("Destroy the ubuntu classic dimension."),
		func() flags.Commander {
			return &cmdDestroyClassic{}
		})
}
*/

func (x *cmdEnableClassic) Execute(args []string) (err error) {
	return x.doEnable()
}

func (x *cmdEnableClassic) doEnable() (err error) {
	if classic.Enabled() {
		return fmt.Errorf(i18n.G("Classic dimension is already enabled."))
	}

	pbar := progress.NewTextProgress()
	if err := classic.Create(pbar); err != nil {
		return err
	}

	fmt.Fprintln(Stdout, i18n.G(`Classic dimension enabled on this snappy system.
Use "snap shell classic" to enter the classic dimension.`))
	return nil
}

func (x *cmdDestroyClassic) Execute(args []string) (err error) {
	return x.doDisable()
}

func (x *cmdDestroyClassic) doDisable() (err error) {
	if !classic.Enabled() {
		return fmt.Errorf(i18n.G("Classic dimension is not enabled."))
	}

	if err := classic.Destroy(); err != nil {
		return err
	}

	fmt.Fprintln(Stdout, i18n.G(`Classic dimension destroyed on this snappy system.`))
	return nil
}
