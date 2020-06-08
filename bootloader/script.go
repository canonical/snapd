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
)

var (
	_ bootScript = (*textBootScript)(nil)
)

// bootScript is a wrapper for boot scripts
type bootScript interface {
	Edition() uint
	Script() []byte
}

var errNoEdition = errors.New("no edition")

// editionFromScriptFile extracts the edition information from a boot script
// file
func editionFromScriptFile(p string) (uint, error) {
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, errNoEdition
		}
		return 0, fmt.Errorf("cannot load existing boot script: %v", err)
	}
	defer f.Close()
	return editionFromScript(f)
}

const editionHeader = "# Snapd-Boot-Script-Edition: "

// editionFromScript extracts edition information from boot script
func editionFromScript(script io.Reader) (uint, error) {
	scanner := bufio.NewScanner(script)
	if !scanner.Scan() {
		err := fmt.Errorf("cannot read boot script: unexpected EOF")
		if sErr := scanner.Err(); sErr != nil {
			err = fmt.Errorf("cannot read boot script: %v", err)
		}
		return 0, err
	}

	line := scanner.Text()
	if !strings.HasPrefix(line, editionHeader) {
		return 0, errNoEdition
	}

	editionStr := line[len(editionHeader):]
	editionStr = strings.TrimSpace(editionStr)
	edition, err := strconv.ParseUint(editionStr, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("cannot parse script edition: %v", err)
	}
	return uint(edition), nil
}

// textBootScript is a simple, textual boot script, used by grub or u-boot
type textBootScript struct {
	text          []byte
	parsedEdition uint
}

func (g *textBootScript) Edition() uint {
	return g.parsedEdition
}

func (g *textBootScript) Script() []byte {
	return g.text
}

func bootScriptFrom(data []byte) (bootScript, error) {
	edition, err := editionFromScript(bytes.NewReader(data))
	if err != nil && err != errNoEdition {
		return nil, err
	}
	gbs := &textBootScript{
		text:          data,
		parsedEdition: edition,
	}
	return gbs, nil
}
