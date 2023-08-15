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
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/store/tooling"
	"github.com/snapcore/snapd/timings"
)

var (
	seedOpen = seed.Open

	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr
)

// CoreOptions provides required and optional options for core preseeding.
type CoreOptions struct {
	// prepare image directory
	PrepareImageDir string
	// key to sign preseeded data with
	PreseedSignKey string
	// optional path to AppArmor kernel features directory
	AppArmorKernelFeaturesDir string
	// optional sysfs overlay
	SysfsOverlay string
}

// preseedCoreOptions holds internal preseeding options for the core case
type preseedCoreOptions struct {
	// input options
	CoreOptions
	// chroot directory to run chroot from
	PreseedChrootDir string
	// stystem label of system to be seededs
	SystemLabel string
	// writable directory
	WritableDir string
	// snapd mount point
	SnapdSnapPath string
	// base snap mount point
	BaseSnapPath string
}

type targetSnapdInfo struct {
	path        string
	preseedPath string
	version     string
}

var (
	getKeypairManager        = signtool.GetKeypairManager
	newToolingStoreFromModel = tooling.NewToolingStoreFromModel
	trusted                  = sysdb.Trusted()
)

func MockTrusted(mockTrusted []asserts.Assertion) (restore func()) {
	prevTrusted := trusted
	trusted = mockTrusted
	return func() {
		trusted = prevTrusted
	}
}

func writePreseedAssertion(artifactDigest []byte, opts *preseedCoreOptions) error {
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
		Trusted:        trusted,
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
	if err := sd.LoadMeta("run", nil, tm); err != nil {
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
	for _, ess := range sd.EssentialSnaps() {
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
		"snaps":             snaps,
	}

	signedAssert, err := adb.Sign(asserts.PreseedType, headers, nil, privKey.PublicKey().ID())
	if err != nil {
		return fmt.Errorf("cannot sign preseed assertion: %v", err)
	}

	tsto, err := newToolingStoreFromModel(model, "")
	if err != nil {
		return err
	}
	tsto.Stdout = Stdout

	newFetcher := func(save func(asserts.Assertion) error) asserts.Fetcher {
		return tsto.AssertionFetcher(adb, save)
	}

	f := seedwriter.MakeSeedAssertionFetcher(newFetcher)
	if err := f.Save(signedAssert); err != nil {
		return fmt.Errorf("cannot fetch assertion: %v", err)
	}

	serialized, err := os.OpenFile(filepath.Join(sysDir, "systems", opts.SystemLabel, "preseed"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer serialized.Close()

	enc := asserts.NewEncoder(serialized)
	for _, aref := range f.Refs() {
		if aref.Type == asserts.PreseedType || aref.Type == asserts.AccountKeyType {
			as, err := aref.Resolve(adb.Find)
			if err != nil {
				return fmt.Errorf("internal error: %v", err)
			}
			if err := enc.Encode(as); err != nil {
				return fmt.Errorf("cannot write assertion %s: %v", aref, err)
			}
		}
	}

	return nil
}
