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

package bootloader

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/logger"
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

	// log an info level message, it is a testing build of snapd anyway
	logger.Noticef("maybe inject boot assets?")

	// is there a marker file at /usr/lib/snapd/ in the snap?
	selfExe := mylog.Check2(maybeInjectOsReadlink("/proc/self/exe"))

	injectPieceRaw := mylog.Check2(os.ReadFile(filepath.Join(filepath.Dir(selfExe), "bootassetstesting")))
	if os.IsNotExist(err) {
		logger.Noticef("no boot asset testing marker")
		return
	}
	if len(injectPieceRaw) == 0 {
		logger.Noticef("boot asset testing snippet is empty")
	}
	injectPiece := strings.TrimSpace(string(injectPieceRaw))

	// with boot assets testing enabled and the marker file present, inject
	// a mock boot config update

	grubBootconfig := assets.Internal("grub.cfg")
	if grubBootconfig == nil {
		panic("no bootconfig")
	}
	snippets := assets.SnippetsForEditions("grub.cfg:static-cmdline")
	if len(snippets) == 0 {
		panic(fmt.Sprintf("cannot obtain internal grub.cfg:static-cmdline snippets"))
	}

	internalEdition := mylog.Check2(editionFromConfigAsset(bytes.NewReader(grubBootconfig)))

	// bump the injected edition number
	injectedEdition := internalEdition + 1

	logger.Noticef("injecting grub boot assets for testing, edition: %v snippet: %q", injectedEdition, injectPiece)

	lastSnippet := string(snippets[len(snippets)-1].Snippet)
	injectedSnippet := lastSnippet + " " + injectPiece
	injectedSnippets := append(snippets,
		assets.ForEditions{FirstEdition: injectedEdition, Snippet: []byte(injectedSnippet)})

	assets.InjectSnippetsForEditions("grub.cfg:static-cmdline", injectedSnippets)

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
