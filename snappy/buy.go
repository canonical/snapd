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

package snappy

import (
	"fmt"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/store"
)

// Buy the given snap name.
func Buy(fullName string, meter progress.Meter) error {
	mStore := NewConfiguredUbuntuStoreSnapRepository()
	state, redirect, err := mStore.Buy(fullName, store.BuyOptions{}, meter, nil)

	fmt.Printf("Purchase state: %s\n", state)
	if redirect != nil {
		fmt.Printf("  Redirect to: %s\n", redirect.RedirectTo)
	}

	return err
}
