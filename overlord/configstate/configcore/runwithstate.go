// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers
// +build !nomanagers

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
	"strings"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/sysconfig"
)

func init() {
	// Most of these handlers are no-op on classic.
	// TODO: consider allowing some of these on classic too?
	// consider erroring on core-only options on classic?

	coreOnly := &flags{coreOnlyConfig: true}

	// capture cloud information
	addWithStateHandler(nil, setCloudInfoWhenSeeding, nil)

	// proxy.{http,https,ftp}
	addWithStateHandler(validateProxyStore, handleProxyConfiguration, coreOnly)

	// resilience.vitality-hint
	addWithStateHandler(validateVitalitySettings, handleVitalityConfiguration, nil)

	// XXX: this should become a FSOnlyHandler. We need to
	// add/implement Changes() to the ConfGetter interface
	// store-certs.*
	addWithStateHandler(validateCertSettings, handleCertConfiguration, nil)

	// users.create.automatic
	addWithStateHandler(validateUsersSettings, handleUserSettings, &flags{earlyConfigFilter: earlyUsersSettingsFilter})

	validateOnly := &flags{validatedOnlyStateConfig: true}
	addWithStateHandler(validateRefreshSchedule, nil, validateOnly)
	addWithStateHandler(validateRefreshRateLimit, nil, validateOnly)
	addWithStateHandler(validateAutomaticSnapshotsExpiration, nil, validateOnly)

	// netplan.*
	addWithStateHandler(validateNetplanSettings, handleNetplanConfiguration, &flags{coreOnlyConfig: true})
}

type withStateHandler struct {
	validateFunc func(config.Conf) error
	handleFunc   func(config.Conf, *fsOnlyContext) error
	configFlags  flags
}

func (h *withStateHandler) validate(cfg config.ConfGetter) error {
	conf := cfg.(config.Conf)
	if h.validateFunc != nil {
		return h.validateFunc(conf)
	}
	return nil
}

func (h *withStateHandler) handle(dev sysconfig.Device, cfg config.ConfGetter, opts *fsOnlyContext) error {
	conf := cfg.(config.Conf)
	if h.handleFunc != nil {
		return h.handleFunc(conf, opts)
	}
	return nil
}

func (h *withStateHandler) needsState() bool {
	return true
}

func (h *withStateHandler) flags() flags {
	return h.configFlags
}

// addWithStateHandler registers functions to validate and handle a subset of
// system config options requiring to access and manipulate state.
func addWithStateHandler(validate func(config.Conf) error, handle func(config.Conf, *fsOnlyContext) error, flags *flags) {
	if handle == nil && (flags == nil || !flags.validatedOnlyStateConfig) {
		panic("cannot have nil handle with addWithStateHandler if validatedOnlyStateConfig flag is not set")
	}
	h := &withStateHandler{
		validateFunc: validate,
		handleFunc:   handle,
	}
	if flags != nil {
		h.configFlags = *flags
	}
	handlers = append(handlers, h)
}

func Run(dev sysconfig.Device, cfg config.Conf) error {
	return applyHandlers(dev, cfg, handlers)
}

func applyHandlers(dev sysconfig.Device, cfg config.Conf, handlers []configHandler) error {
	// check if the changes
	for _, k := range cfg.Changes() {
		switch {
		case strings.HasPrefix(k, "core.store-certs."):
			if !validCertOption(k) {
				return fmt.Errorf("cannot set store ssl certificate under name %q: name must only contain word characters or a dash", k)
			}
		case k == "core.system.network.netplan" || strings.HasPrefix(k, "core.system.network.netplan."):
			if release.OnClassic {
				return fmt.Errorf("cannot set netplan configuration on classic")
			}
		case !supportedConfigurations[k]:
			return fmt.Errorf("cannot set %q: unsupported system option", k)
		}
	}

	for _, h := range handlers {
		if err := h.validate(cfg); err != nil {
			return err
		}
	}

	for _, h := range handlers {
		if h.flags().coreOnlyConfig && dev.Classic() {
			continue
		}
		if err := h.handle(dev, cfg, nil); err != nil {
			return err
		}
	}
	return nil
}

func Early(dev sysconfig.Device, cfg config.Conf, values map[string]interface{}) error {
	early, relevant := applyFilters(func(f flags) filterFunc {
		return f.earlyConfigFilter
	}, values)

	if err := config.Patch(cfg, "core", early); err != nil {
		return err
	}

	return applyHandlers(dev, cfg, relevant)
}
