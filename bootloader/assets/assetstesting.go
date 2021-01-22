// -*- Mode: Go; indent-tabs-mode: t -*-
// +build withbootassetstesting

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"fmt"
)

// InjectInternal injects an internal asset under the given name.
func InjectInternal(name string, data []byte) {
	fmt.Printf("injecting bootloader asset %q\n", name)
	registeredAssets[name] = data
}

func InternalSnippets(name string) []ForEditions {
	return registeredEditionSnippets[name]
}

// InjectSnippetForEditions injects a set of snippets under a given key.
func InjectSnippetForEditions(name string, snippets []ForEditions) {
	fmt.Printf("injecting bootloader asset edition snippets for %q\n", name)

	if err := sanitizeSnippets(snippets); err != nil {
		panic(err)
	}
	registeredEditionSnippets[name] = snippets
}
