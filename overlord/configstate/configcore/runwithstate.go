// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2020-2022 Canonical Ltd
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
	"github.com/snapcore/snapd/overlord/state"
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
	addWithStateHandler(nil, handleProxyConfiguration, coreOnly)
	// proxy.store
	addWithStateHandler(validateProxyStore, handleProxyStore, nil)

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
	addWithStateHandler(validateNetplanSettings, handleNetplanConfiguration, coreOnly)

	// kernel.{,dangerous-}cmdline-append
	addWithStateHandler(validateCmdlineAppend, handleCmdlineAppend, &flags{modeenvOnlyConfig: true})
}

// RunTransaction is an interface describing how to access
// the system configuration state and transaction.
type RunTransaction interface {
	Get(snapName, key string, result interface{}) error
	GetMaybe(snapName, key string, result interface{}) error
	GetPristine(snapName, key string, result interface{}) error
	Task() *state.Task
	Set(snapName, key string, value interface{}) error
	Changes() []string
	State() *state.State
	Commit()
}

// runTransactionImpl holds a transaction with a task that is in charge of
// appliying a change to the configuration. It is used in the context of
// configcore.
type runTransactionImpl struct {
	*config.Transaction
	task *state.Task
}

func (rt *runTransactionImpl) Task() *state.Task {
	return rt.task
}

func NewRunTransaction(tr *config.Transaction, tk *state.Task) RunTransaction {
	runTransaction := &runTransactionImpl{Transaction: tr, task: tk}
	return runTransaction
}

type withStateHandler struct {
	validateFunc func(RunTransaction) error
	handleFunc   func(RunTransaction, *fsOnlyContext) error
	configFlags  flags
}

func (h *withStateHandler) validate(cfg ConfGetter) error {
	conf := cfg.(RunTransaction)
	if h.validateFunc != nil {
		return h.validateFunc(conf)
	}
	return nil
}

func (h *withStateHandler) handle(dev sysconfig.Device, cfg ConfGetter, opts *fsOnlyContext) error {
	conf := cfg.(RunTransaction)
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
func addWithStateHandler(validate func(RunTransaction) error, handle func(RunTransaction, *fsOnlyContext) error, flags *flags) {
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

func Run(dev sysconfig.Device, cfg RunTransaction) error {
	return applyHandlers(dev, cfg, handlers)
}

func applyHandlers(dev sysconfig.Device, cfg RunTransaction, handlers []configHandler) error {
	// check if the changes
	for _, k := range cfg.Changes() {
		switch {
		case strings.HasPrefix(k, "core.store-certs."):
			if !validCertOption(k) {
				return fmt.Errorf("cannot set store ssl certificate under name %q: name must only contain word characters or a dash", k)
			}
		case isNetplanChange(k):
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
		if h.flags().modeenvOnlyConfig && !dev.HasModeenv() {
			continue
		}
		if err := h.handle(dev, cfg, nil); err != nil {
			return err
		}
	}
	return nil
}

func Early(dev sysconfig.Device, cfg RunTransaction, values map[string]interface{}) error {
	early, relevant := applyFilters(func(f flags) filterFunc {
		return f.earlyConfigFilter
	}, values)

	if err := config.Patch(cfg, "core", early); err != nil {
		return err
	}

	return applyHandlers(dev, cfg, relevant)
}
