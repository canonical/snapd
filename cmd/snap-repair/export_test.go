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

package main

import (
	"net/url"
	"time"

	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/httputil"
)

var (
	ParseArgs       = parseArgs
	Run             = run
	ErrStoreOffline = errStoreOffline
)

type RepairConfig = repairConfig

func MockBaseURL(baseurl string) (restore func()) {
	orig := baseURL
	u, err := url.Parse(baseurl)
	if err != nil {
		panic(err)
	}
	baseURL = u
	return func() {
		baseURL = orig
	}
}

func MockFetchRetryStrategy(strategy retry.Strategy) (restore func()) {
	originalFetchRetryStrategy := fetchRetryStrategy
	fetchRetryStrategy = strategy
	return func() {
		fetchRetryStrategy = originalFetchRetryStrategy
	}
}

func MockPeekRetryStrategy(strategy retry.Strategy) (restore func()) {
	originalPeekRetryStrategy := peekRetryStrategy
	peekRetryStrategy = strategy
	return func() {
		peekRetryStrategy = originalPeekRetryStrategy
	}
}

func MockMaxRepairScriptSize(maxSize int) (restore func()) {
	originalMaxSize := maxRepairScriptSize
	maxRepairScriptSize = maxSize
	return func() {
		maxRepairScriptSize = originalMaxSize
	}
}

func MockTrustedRepairRootKeys(keys []*asserts.AccountKey) (restore func()) {
	original := trustedRepairRootKeys
	trustedRepairRootKeys = keys
	return func() {
		trustedRepairRootKeys = original
	}
}

func TrustedRepairRootKeys() []*asserts.AccountKey {
	return trustedRepairRootKeys
}

func (run *Runner) BrandModel() (brand, model string) {
	return run.state.Device.Brand, run.state.Device.Model
}

func (run *Runner) BaseMode() (base, mode string) {
	return run.state.Device.Base, run.state.Device.Mode
}

func (run *Runner) SetStateModified(modified bool) {
	run.stateModified = modified
}

func (run *Runner) SetBrandModel(brand, model string) {
	run.state.Device.Brand = brand
	run.state.Device.Model = model
}

func (run *Runner) TimeLowerBound() time.Time {
	return run.state.TimeLowerBound
}

func (run *Runner) TLSTime() time.Time {
	return httputil.BaseTransport(run.cli).TLSClientConfig.Time()
}

func (run *Runner) Sequence(brand string) []*RepairState {
	return run.state.Sequences[brand]
}

func (run *Runner) SetSequence(brand string, sequence []*RepairState) {
	if run.state.Sequences == nil {
		run.state.Sequences = make(map[string][]*RepairState)
	}
	run.state.Sequences[brand] = sequence
}

func MockDefaultRepairTimeout(d time.Duration) (restore func()) {
	orig := defaultRepairTimeout
	defaultRepairTimeout = d
	return func() {
		defaultRepairTimeout = orig
	}
}

func MockTimeNow(f func() time.Time) (restore func()) {
	origTimeNow := timeNow
	timeNow = f
	return func() { timeNow = origTimeNow }
}

func NewCmdShow(args ...string) *cmdShow {
	cmdShow := &cmdShow{}
	cmdShow.Positional.Repair = args
	return cmdShow
}

func MockOsGetuid(f func() int) (restore func()) {
	origOsGetuid := osGetuid
	osGetuid = f
	return func() { osGetuid = origOsGetuid }
}
