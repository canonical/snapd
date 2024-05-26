// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build withbootassetstesting

/*
 * Copyright (C) 2021 Canonical Ltd
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

package assets

import (
	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
)

// InjectInternal injects an internal asset under the given name.
func InjectInternal(name string, data []byte) {
	logger.Noticef("injecting bootloader asset %q", name)
	registeredAssets[name] = data
}

func SnippetsForEditions(name string) []ForEditions {
	return registeredEditionSnippets[name]
}

// InjectSnippetForEditions injects a set of snippets under a given key.
func InjectSnippetsForEditions(name string, snippets []ForEditions) {
	logger.Noticef("injecting bootloader asset edition snippets for %q", name)
	mylog.Check(sanitizeSnippets(snippets))

	registeredEditionSnippets[name] = snippets
}
