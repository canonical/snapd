// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package snappy

import (
	"crypto"
	"fmt"
	"strings"

	"github.com/ubuntu-core/snappy/asserts"
	"github.com/ubuntu-core/snappy/pkg/squashfs"
)

func demoCheckSnapAssertions(snap *squashfs.Snap) (string, error) {
	defer snap.Close()

	sysAssertDb, err := asserts.OpenSysDatabase()
	if err != nil {
		return "", err
	}

	nameParts := strings.SplitN(snap.Name(), "_", 2)
	snapID := nameParts[0] // XXX: cheat/guess
	size, hashDigest, err := snap.HashDigest(crypto.SHA256)
	if err != nil {
		return "", fmt.Errorf("failed to hash snap: %v", err)
	}
	formattedDigest, err := asserts.EncodeDigest(crypto.SHA256, hashDigest)
	if err != nil {
		return "", err
	}
	a, err := sysAssertDb.Find(asserts.SnapDeclarationType, map[string]string{
		"snap-id":     snapID,
		"snap-digest": formattedDigest,
	})
	if err == asserts.ErrNotFound {
		return "", fmt.Errorf("no assertion found that validates the snap to be installed")
	}
	if err != nil {
		return "", nil
	}
	snapDecl := a.(*asserts.SnapDeclaration)
	if size != snapDecl.SnapSize() {
		return "", fmt.Errorf("size mismatch between snap and assertion")
	}
	return snapDecl.AuthorityID(), nil // XXX: use this as origin
}
