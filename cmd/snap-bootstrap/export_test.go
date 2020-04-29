// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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
	"io"
	"time"

	"github.com/snapcore/snapd/asserts"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/bootstrap"
	"github.com/snapcore/snapd/secboot"
)

var (
	Parser = parser
)

func MockBootstrapRun(f func(string, string, bootstrap.Options) error) (restore func()) {
	oldBootstrapRun := bootstrapRun
	bootstrapRun = f
	return func() {
		bootstrapRun = oldBootstrapRun
	}
}

func MockStdout(newStdout io.Writer) (restore func()) {
	oldStdout := stdout
	stdout = newStdout
	return func() {
		stdout = oldStdout
	}
}

func MockOsutilIsMounted(f func(path string) (bool, error)) (restore func()) {
	oldOsutilIsMounted := osutilIsMounted
	osutilIsMounted = f
	return func() {
		osutilIsMounted = oldOsutilIsMounted
	}
}

func MockTriggerwatchWait(f func(_ time.Duration) error) (restore func()) {
	oldTriggerwatchWait := triggerwatchWait
	triggerwatchWait = f
	return func() {
		triggerwatchWait = oldTriggerwatchWait
	}
}

var DefaultTimeout = defaultTimeout

func MockDefaultMarkerFile(p string) (restore func()) {
	old := defaultMarkerFile
	defaultMarkerFile = p
	return func() {
		defaultMarkerFile = old
	}
}

func MockSecbootUnlockIfEncrypted(f func(name string, lockKeysOnFinish bool) (string, error)) (restore func()) {
	old := secbootUnlockIfEncrypted
	secbootUnlockIfEncrypted = f
	return func() {
		secbootUnlockIfEncrypted = old
	}
}

func MockSecbootMeasureEpoch(f func(h *secboot.SecbootHandle) error) (restore func()) {
	old := secbootMeasureEpoch
	secbootMeasureEpoch = f
	return func() {
		secbootMeasureEpoch = old
	}
}

func MockSecbootMeasureModel(f func(h *secboot.SecbootHandle, model *asserts.Model) error) (restore func()) {
	old := secbootMeasureModel
	secbootMeasureModel = f
	return func() {
		secbootMeasureModel = old
	}
}
