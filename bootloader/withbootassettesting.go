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

package bootloader

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snapdenv"
)

var maybeInjectOsReadlink = os.Readlink

func MockMaybeInjectOsReadlink(m func(string) (string, error)) (restore func()) {
	old := maybeInjectOsReadlink
	maybeInjectOsReadlink = m
	return func() {
		maybeInjectOsReadlink = old
	}
}

func MaybeInjectTestingBootloaderAssets() {
	// this code is ran only when snapd is built with specific testing tag

	if !snapdenv.Testing() {
		return
	}

	fmt.Printf("maybe inject boot assets?\n")

	// is there a marker file at /usr/lib/snapd/ in the snap?
	selfExe, err := maybeInjectOsReadlink("/proc/self/exe")
	if err != nil {
		panic(fmt.Sprintf("cannot readlink: %v", err))
	}
	if !osutil.FileExists(filepath.Join(filepath.Dir(selfExe), "bootassetstesting")) {
		fmt.Printf("no boot asset testing marker\n")
		return
	}

	// with boot assets testing enabled and the marker file present, inject
	// a mock boot config update

	grubBootconfig := assets.Internal("grub.cfg")
	if grubBootconfig == nil {
		panic("no bootconfig")
	}
	snippets := assets.InternalSnippets("grub.cfg:static-cmdline")
	if len(snippets) == 0 {
		panic(fmt.Sprintf("cannot obtain internal grub.cfg:static-cmdline snippets"))
	}

	internalEdition, err := editionFromConfigAsset(bytes.NewReader(grubBootconfig))
	if err != nil {
		panic(fmt.Sprintf("cannot inject boot config for asset: %v", err))
	}
	// bump he injected edition number
	injectedEdition := internalEdition + 1

	fmt.Printf("injecting grub boot assets for testing, edition: %v\n", injectedEdition)

	lastSnippet := string(snippets[len(snippets)-1].Snippet)
	injectedSnippet := lastSnippet + " bootassetstesting"
	injectedSnippets := append(snippets,
		assets.ForEditions{FirstEdition: injectedEdition, Snippet: []byte(injectedSnippet)})

	assets.InjectSnippetForEditions("grub.cfg:static-cmdline", injectedSnippets)

	origGrubBoot := string(grubBootconfig)
	bumpedEdition := strings.Replace(origGrubBoot,
		fmt.Sprintf("%s%d", editionHeader, internalEdition),
		fmt.Sprintf("%s%d", editionHeader, injectedEdition),
		1)
	// see data/grub.cfg for reference
	bumpedCmdlineAndEdition := strings.Replace(bumpedEdition,
		fmt.Sprintf(`set snapd_static_cmdline_args='%s'`, lastSnippet),
		fmt.Sprintf(`set snapd_static_cmdline_args='%s'`, injectedSnippet),
		1)

	assets.InjectInternal("grub.cfg", []byte(bumpedCmdlineAndEdition))
}

func init() {
	MaybeInjectTestingBootloaderAssets()
}
