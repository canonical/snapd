//go:build nobolt

// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2018 Canonical Ltd
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

// Create returns a placeholder database that does nothing.
func Create() (CommandDB, error) {
	return noopWriter{}, nil
}

type noopWriter struct{}

func (noopWriter) AddSnap(snapName, version, summary string, commands []string) error {
	return nil
}

func (noopWriter) Commit() error {
	return nil
}

func (noopWriter) Rollback() error {
	return nil
}

// DumpCommands returns nothing.
func DumpCommands() (map[string]string, error) {
	return nil, nil
}

// Open returns a placeholder finder that finds nothing.
func Open() (Finder, error) {
	return noopFinder{}, nil
}

type noopFinder struct{}

func (noopFinder) FindCommand(command string) ([]Command, error) {
	return nil, nil
}

func (noopFinder) FindPackage(pkgName string) (*Package, error) {
	return nil, nil
}

func (noopFinder) Close() error {
	return nil
}
