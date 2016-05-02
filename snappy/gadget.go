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

// TODO this should be it's own package, but depends on splitting out
// snap.yaml's

package snappy

import (
	"errors"

	"github.com/ubuntu-core/snappy/snap"
)

// getGadget is a convenience function to not go into the details for
// the business logic for a gadget package in every other function
var getGadget = getGadgetImpl

func getGadgetImpl() (*snap.Info, error) {
	gadgets, _ := ActiveSnapsByType(snap.TypeGadget)
	if len(gadgets) == 1 {
		return gadgets[0].Info(), nil
	}

	return nil, errors.New("no gadget snap")
}
