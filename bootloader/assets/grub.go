// -*- Mode: Go; indent-tabs-mode: t -*-

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
	"github.com/snapcore/snapd/arch"
)

var cmdlineForArch = map[string][]ForEditions{
	"amd64": {
		{FirstEdition: 1, Snippet: []byte("console=ttyS0 console=tty1 panic=-1")},
		{FirstEdition: 3, Snippet: []byte("console=ttyS0,115200n8 console=tty1 panic=-1")},
	},
	"arm64": {
		{FirstEdition: 1, Snippet: []byte("panic=-1")},
	},
	"i386": {
		{FirstEdition: 1, Snippet: []byte("console=ttyS0 console=tty1 panic=-1")},
		{FirstEdition: 3, Snippet: []byte("console=ttyS0,115200n8 console=tty1 panic=-1")},
	},
}

func registerGrubSnippets() {
	snippets := cmdlineForArch[arch.DpkgArchitecture()]
	registerSnippetForEditions("grub.cfg:static-cmdline", snippets)
	registerSnippetForEditions("grub-recovery.cfg:static-cmdline", snippets)
}

func init() {
	registerGrubSnippets()
}
