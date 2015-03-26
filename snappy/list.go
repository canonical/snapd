/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

// ListInstalled returns all installed snaps
func ListInstalled() ([]Part, error) {
	m := NewMetaRepository()

	return m.Installed()
}

// ListUpdates returns all snaps with updates
func ListUpdates() ([]Part, error) {
	m := NewMetaRepository()

	return m.Updates()
}
