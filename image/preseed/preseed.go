// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

// Package preseed provides functions for preseeding of classic and UC20
// systems. Preseeding runs snapd in special mode that executes significant
// portion of initial seeding in a chroot environment and stores the resulting
// modifications in the image so that they can be reused and skipped on first boot,
// speeding it up.
package preseed

import (
	"crypto"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/signtool"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/timings"
)

var (
	seedOpen = seed.Open

	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr
)

type preseedOpts struct {
	PrepareImageDir  string
	PreseedChrootDir string
	SystemLabel      string
	WritableDir      string
	PreseedSignKey   string
}

type targetSnapdInfo struct {
	path    string
	version string
}

var SaveAssertion = func(*asserts.Database, asserts.Assertion, *asserts.Model) error {
	panic("SaveAssertion function not set")
}

var getKeypairManager = signtool.GetKeypairManager

func writePreseedAssertion(opts *preseedOpts, artifactDigest []byte) error {
	keypairMgr, err := getKeypairManager()
	if err != nil {
		return err
	}

	key := opts.PreseedSignKey
	if key == "" {
		key = `default`
	}
	privKey, err := keypairMgr.GetByName(key)
	if err != nil {
		// TRANSLATORS: %q is the key name, %v the error message
		return fmt.Errorf(i18n.G("cannot use %q key: %v"), key, err)
	}

	sysDir := filepath.Join(opts.PrepareImageDir, "system-seed")
	sd, err := seedOpen(sysDir, opts.SystemLabel)
	if err != nil {
		return err
	}

	bs := asserts.NewMemoryBackstore()
	adb, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Trusted:        sysdb.Trusted(),
		KeypairManager: keypairMgr,
		Backstore:      bs,
	})
	if err != nil {
		return err
	}

	commitTo := func(b *asserts.Batch) error {
		return b.CommitTo(adb, nil)
	}

	if err := sd.LoadAssertions(adb, commitTo); err != nil {
		return err
	}
	model := sd.Model()

	tm := timings.New(nil)
	if err := sd.LoadMeta(tm); err != nil {
		return err
	}

	snaps := []interface{}{}
	addSnap := func(sn *seed.Snap) {
		preseedSnap := map[string]interface{}{}
		preseedSnap["name"] = sn.SnapName()
		if sn.ID() != "" {
			preseedSnap["id"] = sn.ID()
			preseedSnap["revision"] = sn.PlaceInfo().SnapRevision().String()
		}
		snaps = append(snaps, preseedSnap)
	}

	modeSnaps, err := sd.ModeSnaps("run")
	if err != nil {
		return err
	}
	essSnaps := sd.EssentialSnaps()
	if err != nil {
		return err
	}
	for _, ess := range essSnaps {
		addSnap(ess)
	}
	for _, msnap := range modeSnaps {
		addSnap(msnap)
	}

	base64Digest, err := asserts.EncodeDigest(crypto.SHA3_384, artifactDigest)
	if err != nil {
		return err
	}
	headers := map[string]interface{}{
		"type":              "preseed",
		"authority-id":      model.AuthorityID(),
		"series":            "16",
		"brand-id":          model.BrandID(),
		"model":             model.Model(),
		"system-label":      opts.SystemLabel,
		"artifact-sha3-384": base64Digest,
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
		"revision":          "1",
		"snaps":             snaps,
	}

	signedAssert, err := adb.Sign(asserts.PreseedType, headers, nil, privKey.PublicKey().ID())
	if err != nil {
		return fmt.Errorf("cannot sign preseed asertion: %v", err)
	}

	if err := SaveAssertion(adb, signedAssert, model); err != nil {
		return fmt.Errorf("cannot add preseed assertion: %v", err)
	}

	assertsDir := filepath.Join(sysDir, "systems", opts.SystemLabel, "assertions")
	if err := os.MkdirAll(assertsDir, 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(assertsDir, "preseed"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := asserts.NewEncoder(f)
	if err := enc.Encode(signedAssert); err != nil {
		return fmt.Errorf("cannot save preseed assertion: %v", err)
	}

	return nil
}
