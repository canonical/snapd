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
	flags() flags
}

// flags carries extra flags that influence how the handler is called.
type flags struct {
	// coreOnlyConfig tells Run/FilesystemOnlyApply to apply the config on core
	// systems only.
	coreOnlyConfig bool
	// validatedOnylStateConfig tells that the config requires only validation,
	// its options are applied dynamically elsewhere.
	validatedOnlyStateConfig bool
}

type fsOnlyHandler struct {
	validateFunc func(config.ConfGetter) error
	handleFunc   func(config.ConfGetter, *fsOnlyContext) error
	configFlags  flags
}

var handlers []configHandler

func init() {
	// Most of these handlers are no-op on classic.
	// TODO: consider allowing some of these on classic too?
	// consider erroring on core-only options on classic?

	coreOnly := &flags{coreOnlyConfig: true}

	// watchdog.{runtime-timeout,shutdown-timeout}
	addFSOnlyHandler(validateWatchdogOptions, handleWatchdogConfiguration, coreOnly)

	// Export experimental.* flags to a place easily accessible from snapd helpers.
	addFSOnlyHandler(validateExperimentalSettings, doExportExperimentalFlags, nil)

	// network.disable-ipv6
	addFSOnlyHandler(validateNetworkSettings, handleNetworkConfiguration, coreOnly)

	// service.*.disable
	addFSOnlyHandler(nil, handleServiceDisableConfiguration, coreOnly)

	// system.power-key-action
	addFSOnlyHandler(nil, handlePowerButtonConfiguration, coreOnly)

	// pi-config.*
	addFSOnlyHandler(nil, handlePiConfiguration, coreOnly)

	// system.disable-backlight-service
	addFSOnlyHandler(validateBacklightServiceSettings, handleBacklightServiceConfiguration, coreOnly)

	// journal.persistent
	addFSOnlyHandler(validateJournalSettings, handleJournalConfiguration, coreOnly)
}

// addFSOnlyHandler registers functions to validate and handle a subset of
// system config options that do not require to manipulate state but only
// the file system.
func addFSOnlyHandler(validate func(config.ConfGetter) error, handle func(config.ConfGetter, *fsOnlyContext) error, flags *flags) {
	if handle == nil {
		panic("cannot have nil handle with fsOnlyHandler")
	}
	h := &fsOnlyHandler{
		validateFunc: validate,
		handleFunc:   handle,
	}
	if flags != nil {
		h.configFlags = *flags
	}
	handlers = append(handlers, h)
}

func (h *fsOnlyHandler) needsState() bool {
	return false
}

func (h *fsOnlyHandler) flags() flags {
	return h.configFlags
}

func (h *fsOnlyHandler) validate(cfg config.ConfGetter) error {
	if h.validateFunc != nil {
		return h.validateFunc(cfg)
	}
	return nil
}

func (h *fsOnlyHandler) handle(cfg config.ConfGetter, opts *fsOnlyContext) error {
	// handleFunc is guaranteed to be non-nil by addFSOnlyHandler
	return h.handleFunc(cfg, opts)
}

type FilesystemOnlyApplyOptions struct {
	// Classic is true when the system in rootdir is a classic system
	Classic bool
}

// FilesystemOnlyApply applies filesystem modifications under rootDir, according to the
// cfg configuration. This is a subset of core config options that is important
// early during boot, before all the configuration is applied as part of
// normal execution of configure hook.
func FilesystemOnlyApply(rootDir string, cfg config.ConfGetter, opts *FilesystemOnlyApplyOptions) error {
	if rootDir == "" {
		return fmt.Errorf("internal error: root directory for configcore.FilesystemOnlyApply() not set")
	}

	ctx := &fsOnlyContext{RootDir: rootDir}
	for _, h := range handlers {
		if h.needsState() {
			continue
		}
		if err := h.validate(cfg); err != nil {
			return err
		}
	}

	for _, h := range handlers {
		if h.needsState() {
			continue
		}
		if h.flags().coreOnlyConfig && opts != nil && opts.Classic {
			continue
		}
		if err := h.handle(cfg, ctx); err != nil {
			return err
		}
	}
	return nil
}
