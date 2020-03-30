// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package configcore

import (
	"fmt"

	"github.com/snapcore/snapd/overlord/configstate/config"
)

func init() {
	// Most of these handlers are no-op on classic.
	// TODO: consider allowing some of these on classic too?
	// consider erroring on core-only options on classic?
	// FIXME: ensure the user cannot set "core seed.loaded"

	// capture cloud information
	addConfigStateHandler(nil, setCloudInfoWhenSeeding)

	// proxy.{http,https,ftp}
	addConfigStateHandler(validateProxyStore, handleProxyConfiguration)
	addConfigStateHandler(validateRefreshSchedule, nil)
	addConfigStateHandler(validateRefreshRateLimit, nil)
	addConfigStateHandler(validateAutomaticSnapshotsExpiration, nil)
}

type cfgStateHandler struct {
	validateFunc func(config.Conf) error
	handleFunc   func(config.Conf, *fsOnlyContext) error
}

func (h *cfgStateHandler) validate(cfg config.ConfGetter) error {
	conf := cfg.(config.Conf)
	if h.validateFunc != nil {
		return h.validateFunc(conf)
	}
	return nil
}

func (h *cfgStateHandler) handle(cfg config.ConfGetter, opts *fsOnlyContext) error {
	conf := cfg.(config.Conf)
	if h.handleFunc != nil {
		return h.handleFunc(conf, opts)
	}
	return nil
}

func (h *cfgStateHandler) needsState() bool {
	return true
}

func addConfigStateHandler(validate func(config.Conf) error, handle func(config.Conf, *fsOnlyContext) error) {
	handlers = append(handlers, &cfgStateHandler{
		validateFunc: validate,
		handleFunc:   handle,
	})
}

func Run(cfg config.Conf) error {
	// check if the changes
	for _, k := range cfg.Changes() {
		if !supportedConfigurations[k] {
			return fmt.Errorf("cannot set %q: unsupported system option", k)
		}
	}

	for _, h := range handlers {
		if err := h.validate(cfg); err != nil {
			return err
		}
		if err := h.handle(cfg, nil); err != nil {
			return err
		}
	}
	return nil
}
