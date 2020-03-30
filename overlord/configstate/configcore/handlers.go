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

type configHandler interface {
	validate(config.ConfGetter) error
	handle(config.ConfGetter, *fsOnlyContext) error
	needsState() bool
}

type cfgHandler struct {
	validateFunc func(config.ConfGetter) error
	handleFunc   func(config.ConfGetter, *fsOnlyContext) error
}

var handlers []configHandler

func init() {
	// Most of these handlers are no-op on classic.
	// TODO: consider allowing some of these on classic too?
	// consider erroring on core-only options on classic?
	// FIXME: ensure the user cannot set "core seed.loaded"

	// do not need state

	// watchdog.{runtime-timeout,shutdown-timeout}
	addConfigHandler(validateWatchdogOptions, handleWatchdogConfiguration)

	// Export experimental.* flags to a place easily accessible from snapd helpers.
	addConfigHandler(validateExperimentalSettings, doExportExperimentalFlags)

	// network.disable-ipv6
	addConfigHandler(validateNetworkSettings, handleNetworkConfiguration)

	// service.*.disable
	addConfigHandler(nil, handleServiceDisableConfiguration)

	// system.power-key-action
	addConfigHandler(nil, handlePowerButtonConfiguration)

	// pi-config.*
	addConfigHandler(nil, handlePiConfiguration)
}

func addConfigHandler(validate func(config.ConfGetter) error, handle func(config.ConfGetter, *fsOnlyContext) error) {
	handlers = append(handlers, &cfgHandler{
		validateFunc: validate,
		handleFunc:   handle,
	})
}

func (h *cfgHandler) needsState() bool {
	return false
}

func (h *cfgHandler) validate(cfg config.ConfGetter) error {
	if h.validateFunc != nil {
		return h.validateFunc(cfg)
	}
	return nil
}

func (h *cfgHandler) handle(cfg config.ConfGetter, opts *fsOnlyContext) error {
	if h.handleFunc != nil {
		return h.handleFunc(cfg, opts)
	}
	return nil
}

// FilesystemOnlyApply applies filesystem modifications under rootDir, according to the
// cfg configuration. This is a subset of core config options that is important
// early during boot, before all the configuration is applied as part of
// normal execution of configure hook.
func FilesystemOnlyApply(rootDir string, cfg config.ConfGetter) error {
	if rootDir == "" {
		return fmt.Errorf("internal error: root directory for configcore.FilesystemOnlyApply() not set")
	}

	opts := &fsOnlyContext{RootDir: rootDir}
	for _, h := range handlers {
		if h.needsState() {
			continue
		}
		if err := h.validate(cfg); err != nil {
			return err
		}
		if err := h.handle(cfg, opts); err != nil {
			return err
		}
	}
	return nil
}
