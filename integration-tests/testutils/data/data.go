// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
	BaseSnapPath = "integration-tests/data/snaps"
	// BasicSnapName is the name of the basic snap
	BasicSnapName = "basic"
	// BasicBinariesSnapName is the name of the basic snap with binaries
	BasicBinariesSnapName = "basic-binaries"
	// BasicConfigSnapName is the name of the basic snap with config hook
	BasicConfigSnapName = "basic-config"
	// BasicServiceSnapName is the name of the basic snap with a service
	BasicServiceSnapName = "basic-service"
	// BasicDesktopSnapName is the name of the snap with a desktop file
	BasicDesktopSnapName = "basic-desktop"
	// NetworkConsumerSnapName is the name of the snap with network plug
	NetworkConsumerSnapName = "network-consumer"
	// NetworkBindConsumerSnapName is the name of the snap with network plug
	NetworkBindConsumerSnapName = "network-bind-consumer"
	// HomeConsumerSnapName is the name of the snap with home plug
	HomeConsumerSnapName = "home-consumer"
	// WrongYamlSnapName is the name of a snap with an invalid meta yaml
	WrongYamlSnapName = "wrong-yaml"
)
