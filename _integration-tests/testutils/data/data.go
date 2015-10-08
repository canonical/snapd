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

package data

const (
	// BaseSnapPath is the path for the snap sources used in testing
	BaseSnapPath = "_integration-tests/data/snaps"
	// BasicSnapName is the name of the basic snap
	BasicSnapName = "basic"
	// BasicWithBinariesSnapName is the name of the basic snap with binaries
	BasicWithBinariesSnapName = "basic-with-binaries"
	// WrongYamlSnapName is the name of a snap with an invalid meta yaml
	WrongYamlSnapName = "wrong-yaml"
	// MissingReadmeSnapName is the name of a snap without readme
	MissingReadmeSnapName = "missing-readme"
)
