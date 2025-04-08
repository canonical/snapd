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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
)

func snapNameFromPath(snapPath string) string {
	return strings.Split(filepath.Base(snapPath), "_")[0]
}

// TODO: also support reading/copying form a store snap

func NewSnapRevision(targetDir string, snap string, headers map[string]any) (string, error) {
	db, err := newAssertsDB(systestkeys.TestStorePrivKey)
	if err != nil {
		return "", err
	}
	digest, size, err := asserts.SnapFileSHA3_384(snap)
	if err != nil {
		return "", err
	}

	fallbacks := map[string]any{
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

func NewSnapResourceRevision(targetDir string, compPath string, headers map[string]any) (string, error) {
	db, err := newAssertsDB(systestkeys.TestStorePrivKey)
	if err != nil {
		return "", err
	}
	digest, size, err := asserts.SnapFileSHA3_384(compPath)
	if err != nil {
		return "", err
	}

	container, err := snapfile.Open(compPath)
	if err != nil {
		return "", err
	}

	ci, err := snap.ReadComponentInfoFromContainer(container, nil, nil)
	if err != nil {
		return "", err
	}

	required := []string{"snap-id", "resource-revision"}
	for _, r := range required {
		if _, ok := headers[r]; !ok {
			return "", fmt.Errorf("missing required header %q", r)
		}
	}

	defaults := map[string]any{
		"type":              "snap-resource-revision",
		"authority-id":      "testrootorg",
		"developer-id":      "testrootorg",
		"resource-name":     ci.Component.ComponentName,
		"timestamp":         time.Now().Format(time.RFC3339),
		"resource-size":     fmt.Sprintf("%d", size),
		"resource-sha3-384": digest,
	}
	for k, v := range defaults {
		if _, ok := headers[k]; !ok {
			headers[k] = v
		}
	}
	headers["authority-id"] = "testrootorg"
	headers["snap-sha3-384"] = digest
	headers["snap-size"] = fmt.Sprintf("%d", size)
	headers["timestamp"] = time.Now().Format(time.RFC3339)

	a, err := db.Sign(asserts.SnapResourceRevisionType, headers, nil, systestkeys.TestStoreKeyID)
	if err != nil {
		return "", err
	}
	return writeAssert(a, targetDir)
}

func NewSnapResourcePair(targetDir string, compPath string, headers map[string]any) (string, error) {
	db, err := newAssertsDB(systestkeys.TestStorePrivKey)
	if err != nil {
		return "", err
	}

	container, err := snapfile.Open(compPath)
	if err != nil {
		return "", err
	}

	ci, err := snap.ReadComponentInfoFromContainer(container, nil, nil)
	if err != nil {
		return "", err
	}

	required := []string{"snap-id", "resource-revision", "snap-revision"}
	for _, r := range required {
		if _, ok := headers[r]; !ok {
			return "", fmt.Errorf("missing required header %q", r)
		}
	}

	defaults := map[string]any{
		"type":          "snap-resource-pair",
		"authority-id":  "testrootorg",
		"developer-id":  "testrootorg",
		"resource-name": ci.Component.ComponentName,
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	for k, v := range defaults {
		if _, ok := headers[k]; !ok {
			headers[k] = v
		}
	}

	a, err := db.Sign(asserts.SnapResourcePairType, headers, nil, systestkeys.TestStoreKeyID)
	if err != nil {
		return "", err
	}
	return writeAssert(a, targetDir)
}

func NewSnapDeclaration(targetDir string, snap string, headers map[string]any) (string, error) {
	db, err := newAssertsDB(systestkeys.TestStorePrivKey)
	if err != nil {
		return "", err
	}

	fallbacks := map[string]any{
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
func NewRepair(targetDir string, scriptFilename string, headers map[string]any) (string, error) {
	// use the separate root of trust for signing repair assertions
	db, err := newAssertsDB(systestkeys.TestRepairRootPrivKey)
	if err != nil {
		return "", err
	}

	fallbacks := map[string]any{
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
	headers["series"] = []any{"16"}
	headers["timestamp"] = time.Now().Format(time.RFC3339)

	a, err := db.Sign(asserts.RepairType, headers, scriptBodyBytes, systestkeys.TestRepairKeyID)
	if err != nil {
		return "", err
	}

	return writeAssert(a, targetDir)
}
