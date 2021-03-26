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
	"github.com/snapcore/snapd/sysconfig"
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
	// validatedOnlyStateConfig tells that the config requires only validation,
	// its options are applied dynamically elsewhere.
	validatedOnlyStateConfig bool
	// earlyConfigFilter expresses whether the handler supports
	// any early configuration options (that can and must be
	// set before even seeding is finished).
	// If set the function should copy such options from values
	// to early.
	earlyConfigFilter filterFunc

	// preinstallFilter is similar to earlyConfigFilter in that
	// it allows a handler to express that certain configuration options should
	// be applied to the filesystem directory indicated by fsOnlyContext.RootDir
	// in the  stage before the image is booted (called "preinstall"). This
	// filesystem for UC18 can be a full root filesystem, but for UC20 this
	// filesystem will be just a recovery system.
	// This is primarily meant for gadget assets such as config.txt on the Pi
	// that need to be processed before the image is even booted (since the Pi
	// GPU bootloader is what processes config.txt, modifying at runtime after
	// booting is too late and would require a reboot to take effect).
	// Note that this is currently only used for UC20 preinstall.
	preinstallFilter filterFunc
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
	addFSOnlyHandler(validateExperimentalSettings, doExportExperimentalFlags, &flags{earlyConfigFilter: earlyExperimentalSettingsFilter})

	// network.disable-ipv6
	addFSOnlyHandler(validateNetworkSettings, handleNetworkConfiguration, coreOnly)

	// service.*.disable
	addFSOnlyHandler(nil, handleServiceDisableConfiguration, coreOnly)

	// system.power-key-action
	addFSOnlyHandler(nil, handlePowerButtonConfiguration, coreOnly)

	// pi-config.*
	addFSOnlyHandler(nil, handlePiConfiguration, &flags{coreOnlyConfig: true, preinstallFilter: preinstallPiSettingsFilter})

	// system.disable-backlight-service
	addFSOnlyHandler(validateBacklightServiceSettings, handleBacklightServiceConfiguration, coreOnly)

	// system.kernel.printk.console-loglevel
	addFSOnlyHandler(validateSysctlOptions, handleSysctlConfiguration, coreOnly)

	// journal.persistent
	addFSOnlyHandler(validateJournalSettings, handleJournalConfiguration, coreOnly)

	// system.timezone
	addFSOnlyHandler(validateTimezoneSettings, handleTimezoneConfiguration, coreOnly)

	sysconfig.ApplyFilesystemOnlyDefaultsImpl = func(rootDir string, defaults map[string]interface{}, options *sysconfig.FilesystemOnlyApplyOptions) error {
		return filesystemOnlyApply(rootDir, defaults, options)
	}

	sysconfig.ApplyPreinstallFilesystemOnlyDefaultsImpl = func(rootDir string, defaults map[string]interface{}, options *sysconfig.FilesystemOnlyApplyOptions) error {
		return preinstallFilesystemOnlyApply(rootDir, defaults, options)
	}
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

// filesystemOnlyApply applies filesystem modifications under rootDir, according to the
// cfg configuration. This is a subset of core config options that is important
// early during boot, before all the configuration is applied as part of
// normal execution of configure hook.
// Exposed for use via sysconfig.ApplyFilesystemOnlyDefaults.
func filesystemOnlyApply(rootDir string, values map[string]interface{}, opts *sysconfig.FilesystemOnlyApplyOptions) error {
	if rootDir == "" {
		return fmt.Errorf("internal error: root directory for configcore.FilesystemOnlyApply() not set")
	}

	if opts == nil {
		opts = &sysconfig.FilesystemOnlyApplyOptions{}
	}

	cfg := plainCoreConfig(values)

	ctx := &fsOnlyContext{
		RootDir: rootDir,
	}
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

// preinstallFilesystemOnlyApply is like filesystemOnlyApply, except that it is
// only meant to be called before the installation happens, when the image is
// constructed. This enables certain settings like the pi-config options to
// be applied to things in the image that must happen before booting, typically
// bootloader related settings that cannot be applied at runtime without a
// reboot.
// The first argument here can either be a recovery system on UC20, or it could
// be an actual root filesystem in the UC18 case, though we have not yet enabled
// this for UC18, but the code does support it.
func preinstallFilesystemOnlyApply(systemDir string, values map[string]interface{}, opts *sysconfig.FilesystemOnlyApplyOptions) error {
	if systemDir == "" {
		return fmt.Errorf("internal error: root directory for configcore.FilesystemOnlyApply() not set")
	}

	if opts == nil {
		opts = &sysconfig.FilesystemOnlyApplyOptions{}
	}

	// filter the values to only keep values that use the preinstall flag
	preinstall, preinstallHandlers := applyFilters(func(f flags) filterFunc {
		return f.preinstallFilter
	}, values)

	// use only the filtered keys that we got in the above loop for the config
	cfg := plainCoreConfig(preinstall)

	// validate all of the keys that are relevant to preinstallation against
	// all of the handlers that are relevant to preinstallation
	for _, h := range preinstallHandlers {
		if h.needsState() {
			// it doesn't make sense to have a handler which needs state, but
			// is also called in a preinstallation context
			return fmt.Errorf("internal error: handler needs state but is used in preinstall context")
		}

		if err := h.validate(cfg); err != nil {
			return err
		}
	}

	ctx := &fsOnlyContext{
		RootDir: systemDir,
		// TODO: currently we always set this to true, but the code works on
		//       UC18 for example if we decided on how to pass in that info to
		//       this function
		UC20Recovery: true,
	}
	for _, h := range preinstallHandlers {
		if h.flags().coreOnlyConfig && opts != nil && opts.Classic {
			continue
		}

		if err := h.handle(cfg, ctx); err != nil {
			return err
		}
	}

	return nil
}
