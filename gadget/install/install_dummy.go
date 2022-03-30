// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build nosecboot
// +build nosecboot

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package install

import (
	"fmt"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/timings"
)

func Run(model gadget.Model, gadgetRoot, kernelRoot, device string, options Options, _ gadget.ContentObserver, _ timings.Measurer) (*InstalledSystemSideData, error) {
	return nil, fmt.Errorf("build without secboot support")
}

func FactoryReset(model gadget.Model, gadgetRoot, kernelRoot, device string, options Options, _ gadget.ContentObserver, _ timings.Measurer) (*InstalledSystemSideData, error) {
	return nil, fmt.Errorf("build without secboot support")
}
