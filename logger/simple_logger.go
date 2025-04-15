// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !structuredlogging

/*
 * Copyright (C) 2025 Canonical Ltd
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

package logger

import (
	"io"
)

// New creates a log.Logger using the given io.Writer and flag, using the
// options from opts.
func New(w io.Writer, flag int, opts *LoggerOptions) Logger {
	if opts == nil {
		opts = &LoggerOptions{}
	}
	return newLog(w, flag, opts)
}
