// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package seccomp

// MockTemplate replaces seccomp template.
//
// NOTE: The real seccomp template is long. For testing it is convenient for
// replace it with a shorter snippet.
func MockTemplate(fakeTemplate []byte) (restore func()) {
	orig := defaultTemplate
	defaultTemplate = fakeTemplate
	return func() { defaultTemplate = orig }
}
