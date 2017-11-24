// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package corecfg

import (
	"fmt"
	"os"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

var (
	Stdout = os.Stdout
	Stderr = os.Stderr
)

type Conf interface {
	Get(key string, result interface{}) error
	State() *state.State
}

func coreCfg(ctx Conf, key string) (result string, err error) {
	var v interface{} = ""
	if err := ctx.Get(key, &v); err != nil && err != state.ErrNoState {
		return "", err
	}
	// TODO: we could have a fully typed approach but at the
	// moment we also always use "" to mean unset as well, this is
	// the smallest change
	return fmt.Sprintf("%v", v), nil
}

func Run(ctx Conf) error {
	if err := validateProxyStore(ctx); err != nil {
		return err
	}
	if err := validateRefreshSchedule(ctx); err != nil {
		return err
	}

	// see if it makes sense to run at all
	if release.OnClassic {
		// nothing to do
		return nil
	}
	// TODO: consider allowing some of these on classic too?
	// consider erroring on core-only options on classic?

	// handle the various core config options:
	// service.*.disable
	if err := handleServiceDisableConfiguration(ctx); err != nil {
		return err
	}
	// system.power-key-action
	if err := handlePowerButtonConfiguration(ctx); err != nil {
		return err
	}
	// pi-config.*
	if err := handlePiConfiguration(ctx); err != nil {
		return err
	}
	// proxy.{http,https,ftp}
	if err := handleProxyConfiguration(ctx); err != nil {
		return err
	}

	return nil
}
