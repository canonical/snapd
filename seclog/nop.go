// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package seclog

// nopLogger provides a no-operation [securityLogger] implementation.
type nopLogger struct{}

// Ensure [nopLogger] implements [securityLogger].
var _ securityLogger = (*nopLogger)(nil)

func newNopLogger() securityLogger {
	return nopLogger{}
}

// LogLoggingEnabled implements [securityLogger.LogLoggingEnabled].
func (nopLogger) LogLoggingEnabled() {
}

// LogLoggingDisabled implements [securityLogger.LogLoggingDisabled].
func (nopLogger) LogLoggingDisabled() {
}

// LogLoginSuccess implements [securityLogger.LogLoginSuccess].
func (nopLogger) LogLoginSuccess(user SnapdUser) {
}

// LogLoginFailure implements [securityLogger.LogLoginFailure].
func (nopLogger) LogLoginFailure(user SnapdUser, reason Reason) {
}

// LogUserCreated implements [securityLogger.LogUserCreated].
func (nopLogger) LogUserCreated(user SnapdUser) {
}

// LogUserUpdated implements [securityLogger.LogUserUpdated].
func (nopLogger) LogUserUpdated(user SnapdUser, changedFields []string) {
}

// LogUserRemoved implements [securityLogger.LogUserRemoved].
func (nopLogger) LogUserRemoved(user SnapdUser) {
}
