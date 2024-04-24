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
	"os"
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

func NewSnapRevision(targetDir string, snap string, headers map[string]interface{}) (string, error) {
	db, err := newAssertsDB(systestkeys.TestStorePrivKey)
	if err != nil {
		return "", err
	}
	digest, size, err := asserts.SnapFileSHA3_384(snap)
	if err != nil {
		return "", err
	}

	fallbacks := map[string]interface{}{
		"developer-id":  "testrootorg",
		"snap-id":       snapNameFromPath(snap) + "-id",
		"snap-revision": "1",
	}
	for k, v := range fallbacks {
		if _, ok := headers[k]; !ok {
			headers[k] = v
		}
	}
	headers["authority-id"] = "testrootorg"
	headers["snap-sha3-384"] = digest
	headers["snap-size"] = fmt.Sprintf("%d", size)
	headers["timestamp"] = time.Now().Format(time.RFC3339)

	a, err := db.Sign(asserts.SnapRevisionType, headers, nil, systestkeys.TestStoreKeyID)
	if err != nil {
		return "", err
	}
	return writeAssert(a, targetDir)
}

func NewSnapDeclaration(targetDir string, snap string, headers map[string]interface{}) (string, error) {
	db, err := newAssertsDB(systestkeys.TestStorePrivKey)
	if err != nil {
		return "", err
	}

	fallbacks := map[string]interface{}{
		"snap-id":      snapNameFromPath(snap) + "-id",
		"snap-name":    snapNameFromPath(snap),
		"publisher-id": "testrootorg",
	}
	for k, v := range fallbacks {
		if _, ok := headers[k]; !ok {
			headers[k] = v
		}
	}
	headers["authority-id"] = "testrootorg"
	headers["series"] = "16"
	headers["timestamp"] = time.Now().Format(time.RFC3339)

	a, err := db.Sign(asserts.SnapDeclarationType, headers, nil, systestkeys.TestStoreKeyID)
	if err != nil {
		return "", err
	}
	return writeAssert(a, targetDir)
}

// NewRepair signs a repair assertion, including the specified script as the
// body of the assertion.
func NewRepair(targetDir string, scriptFilename string, headers map[string]interface{}) (string, error) {
	// use the separate root of trust for signing repair assertions
	db, err := newAssertsDB(systestkeys.TestRepairRootPrivKey)
	if err != nil {
		return "", err
	}

	fallbacks := map[string]interface{}{
		"brand-id":  "testrootorg",
		"repair-id": "1",
		"summary":   "some test keys repair",
	}
	for k, v := range fallbacks {
		if _, ok := headers[k]; !ok {
			headers[k] = v
		}
	}

	scriptBodyBytes, err := os.ReadFile(scriptFilename)
	if err != nil {
		return "", err
	}

	headers["authority-id"] = "testrootorg"
	// note that series is a list for repair assertions
	headers["series"] = []interface{}{"16"}
	headers["timestamp"] = time.Now().Format(time.RFC3339)

	a, err := db.Sign(asserts.RepairType, headers, scriptBodyBytes, systestkeys.TestRepairKeyID)
	if err != nil {
		return "", err
	}

	return writeAssert(a, targetDir)
}
