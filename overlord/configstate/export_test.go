// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

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

package configstate

import (
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/sysconfig"
)

var (
	NewConfigureHandler        = newConfigureHandler
	NewDefaultConfigureHandler = newDefaultConfigureHandler
)

func MockConfigcoreExportExperimentalFlags(mock func(tr configcore.ConfGetter) error) (restore func()) {
	old := configcoreExportExperimentalFlags
	configcoreExportExperimentalFlags = mock
	return func() {
		configcoreExportExperimentalFlags = old
	}
}

func MockConfigcoreEarly(mock func(dev sysconfig.Device, cfg configcore.RunTransaction, values map[string]interface{}) error) (restore func()) {
	old := configcoreEarly
	configcoreEarly = mock
	return func() {
		configcoreEarly = old
	}
}
