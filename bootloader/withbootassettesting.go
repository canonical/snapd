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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

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

func MaybeInjectTestingBootloaderAssets(role Role) {
	// this code is ran only when snapd is built with specific testing tag

	if !snapdenv.Testing() {
		return
	}

	// log an info level message, it is a testing build of snapd anyway
	logger.Noticef("maybe inject boot assets?")

	markerFile := "bootassetstesting"
	grubCfgAsset := "grub.cfg"
	if role == RoleRecovery {
		markerFile = "recoverybootassetstesting"
		grubCfgAsset = "grub-recovery.cfg"
	}

	// is there a marker file at /usr/lib/snapd/ in the snap?
	selfExe, err := maybeInjectOsReadlink("/proc/self/exe")
	if err != nil {
		panic(fmt.Sprintf("cannot readlink: %v", err))
	}

	injectPieceRaw, err := ioutil.ReadFile(filepath.Join(filepath.Dir(selfExe), markerFile))
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

	grubBootconfig := assets.Internal(grubCfgAsset)
	if grubBootconfig == nil {
		panic("no bootconfig")
	}
	snippetName := grubCfgAsset + ":static-cmdline"
	snippets := assets.SnippetsForEditions(snippetName)
	if len(snippets) == 0 {
		panic(fmt.Sprintf("cannot obtain internal %s snippets", snippetName))
	}

	internalEdition, err := editionFromConfigAsset(bytes.NewReader(grubBootconfig))
	if err != nil {
		panic(fmt.Sprintf("cannot inject boot config for asset: %v", err))
	}
	// bump the injected edition number
	injectedEdition := internalEdition + 1

	logger.Noticef("injecting grub boot assets for testing, edition: %v snippet: %q",
		injectedEdition, injectPiece)

	lastSnippet := string(snippets[len(snippets)-1].Snippet)
	injectedSnippet := lastSnippet + " " + injectPiece
	injectedSnippets := append(snippets,
		assets.ForEditions{FirstEdition: injectedEdition, Snippet: []byte(injectedSnippet)})

	assets.InjectSnippetsForEditions(snippetName, injectedSnippets)

	origGrubBoot := string(grubBootconfig)
	bumpedEdition := strings.Replace(origGrubBoot,
		fmt.Sprintf("%s%d", editionHeader, internalEdition),
		fmt.Sprintf("%s%d", editionHeader, injectedEdition),
		1)
	// see data/grub{-recovery}.cfg for reference
	bumpedCmdlineAndEdition := strings.Replace(bumpedEdition,
		fmt.Sprintf(`set snapd_static_cmdline_args='%s'`, lastSnippet),
		fmt.Sprintf(`set snapd_static_cmdline_args='%s'`, injectedSnippet),
		1)

	assets.InjectInternal(grubCfgAsset, []byte(bumpedCmdlineAndEdition))
}

func init() {
	MaybeInjectTestingBootloaderAssets(RoleRunMode)
	MaybeInjectTestingBootloaderAssets(RoleRecovery)
}
