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

package bootloader

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/bootloader/assets"
)

var errNoEdition = errors.New("no edition")

// editionFromDiskConfigAsset extracts the edition information from a boot
// config asset on disk.
func editionFromDiskConfigAsset(p string) (uint, error) {
	f := mylog.Check2(os.Open(p))

	defer f.Close()
	return editionFromConfigAsset(f)
}

const editionHeader = "# Snapd-Boot-Config-Edition: "

// editionFromConfigAsset extracts edition information from boot config asset.
func editionFromConfigAsset(asset io.Reader) (uint, error) {
	scanner := bufio.NewScanner(asset)
	if !scanner.Scan() {
		mylog.Check(fmt.Errorf("cannot read config asset: unexpected EOF"))
		if sErr := scanner.Err(); sErr != nil {
			mylog.Check(fmt.Errorf("cannot read config asset: %v", err))
		}
		return 0, err
	}

	line := scanner.Text()
	if !strings.HasPrefix(line, editionHeader) {
		return 0, errNoEdition
	}

	editionStr := line[len(editionHeader):]
	editionStr = strings.TrimSpace(editionStr)
	edition := mylog.Check2(strconv.ParseUint(editionStr, 10, 32))

	return uint(edition), nil
}

// editionFromInternalConfigAsset extracts edition information from a named
// internal boot config asset.
func editionFromInternalConfigAsset(assetName string) (uint, error) {
	data := assets.Internal(assetName)
	if data == nil {
		return 0, fmt.Errorf("internal error: no boot asset for %q", assetName)
	}
	return editionFromConfigAsset(bytes.NewReader(data))
}

// configAsset is a boot config asset, such as text script, used by grub or
// u-boot.
type configAsset struct {
	body          []byte
	parsedEdition uint
}

func (g *configAsset) Edition() uint {
	return g.parsedEdition
}

func (g *configAsset) Raw() []byte {
	return g.body
}

func configAssetFrom(data []byte) (*configAsset, error) {
	edition := mylog.Check2(editionFromConfigAsset(bytes.NewReader(data)))
	if err != nil && err != errNoEdition {
		return nil, err
	}
	gbs := &configAsset{
		body:          data,
		parsedEdition: edition,
	}
	return gbs, nil
}
