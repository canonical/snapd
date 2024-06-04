// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build withtestkeys

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/systestkeys"
)

func init() {
	// when built with testkeys enabled, trust the TestRepairRootAccountKey
	trustedRepairRootKeys = append(trustedRepairRootKeys, systestkeys.TestRepairRootAccountKey.(*asserts.AccountKey))

	// also check for root brand ID
	rootBrandIDs = append(rootBrandIDs, "testrootorg")
}
