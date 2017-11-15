// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package refresh

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/systestkeys"
)

func snapNameFromPath(snapPath string) string {
	return strings.Split(filepath.Base(snapPath), "_")[0]
}

// TODO: also support reading/copying form a store snap

func NewSnapRevision(targetDir string, snap string, headers map[string]interface{}) error {
	db, err := newAssertsDB()
	if err != nil {
		return err
	}
	digest, size, err := asserts.SnapFileSHA3_384(snap)
	if err != nil {
		return err
	}

	headers["authority-id"] = "testrootorg"
	headers["snap-sha3-384"] = digest
	if _, ok := headers["developer-id"]; !ok {
		headers["developer-id"] = "testrootorg"
	}
	if _, ok := headers["snap-id"]; !ok {
		headers["snap-id"] = snapNameFromPath(snap) + "-id"
	}
	if _, ok := headers["snap-revision"]; !ok {
		headers["snap-revision"] = "42"
	}
	headers["snap-size"] = fmt.Sprintf("%d", size)
	headers["timestamp"] = time.Now().Format(time.RFC3339)

	a, err := db.Sign(asserts.SnapRevisionType, headers, nil, systestkeys.TestStoreKeyID)
	if err != nil {
		return err
	}
	return writeAssert(a, targetDir)
}

func NewSnapDeclaration(targetDir string, snap string, headers map[string]interface{}) error {
	db, err := newAssertsDB()
	if err != nil {
		return err
	}

	if _, ok := headers["snap-name"]; !ok {
		headers["snap-name"] = snapNameFromPath(snap)
	}

	headers["authority-id"] = "testrootorg"
	headers["series"] = "16"
	if _, ok := headers["snap-id"]; !ok {
		headers["snap-id"] = snapNameFromPath(snap) + "-id"
	}
	if _, ok := headers["publisher-id"]; !ok {
		headers["publisher-id"] = "testrootorg"
	}
	if _, ok := headers["snap-name"]; !ok {
		headers["snap-name"] = snapNameFromPath(snap)
	}
	headers["timestamp"] = time.Now().Format(time.RFC3339)

	a, err := db.Sign(asserts.SnapDeclarationType, headers, nil, systestkeys.TestStoreKeyID)
	if err != nil {
		return err
	}
	return writeAssert(a, targetDir)
}
