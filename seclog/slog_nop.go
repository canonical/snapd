// -*- Mode: Go; indent-tabs-mode: t -*-

// Fallback for environments where log/slog is not available (Go < 1.21
// or the noslog build tag is set). NewSlogLogger returns a nop logger
// so that callers compile unconditionally.
//go:build !go1.21 || noslog

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

import "io"

// NewSlogLogger returns a nop logger when log/slog is not available.
// The writer, appID and minLevel parameters are accepted for API
// compatibility but are ignored.
func NewSlogLogger(_ io.Writer, _ string, _ Level) SecurityLogger {
	return NewNopLogger()
}
