// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2024 Canonical Ltd
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

package advisor

import "errors"

type CommandDB interface {
	// AddSnap adds the entries for commands pointing to the given
	// snap name to the commands database.
	AddSnap(snapName, version, summary string, commands []string) error
	// Commit persist the changes, and closes the database. If the
	// database has already been committed/rollbacked, does nothing.
	Commit() error
	// Rollback aborts the changes, and closes the database. If the
	// database has already been committed/rollbacked, does nothing.
	Rollback() error
}

// ErrNotSupported indicates that advisor is not supported.
var ErrNotSupported = errors.New("advisor is not supported")
