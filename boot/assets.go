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

package boot

import (
	"crypto"
	_ "crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

type trustedAssetsCache struct {
	cacheDir string
	hash     crypto.Hash
}

func newTrustedAssetsCache(cacheDir string) *trustedAssetsCache {
	return &trustedAssetsCache{cacheDir: cacheDir, hash: crypto.SHA256}
}

func (c *trustedAssetsCache) assetKey(blName, assetName, assetHash string) string {
	return filepath.Join(blName, fmt.Sprintf("%s-%s", assetName, assetHash))
}

func (c *trustedAssetsCache) tempAssetKey(blName, assetName string) string {
	return filepath.Join(blName, assetName+".temp")
}

func (c *trustedAssetsCache) pathInCache(part string) string {
	return filepath.Join(c.cacheDir, part)
}

// Add entry for
func (c *trustedAssetsCache) Add(assetPath, blName, assetName string) (ta *trackedAsset, added bool, err error) {
	if err := os.MkdirAll(c.pathInCache(blName), 0755); err != nil {
		return nil, false, fmt.Errorf("cannot create cache directory: %v", err)
	}

	// input
	inf, err := os.Open(assetPath)
	if err != nil {
		return nil, false, fmt.Errorf("cannot open asset file: %v", err)
	}
	// temporary output
	tempPath := c.pathInCache(c.tempAssetKey(blName, assetName))
	outf, err := osutil.NewAtomicFile(tempPath, 0644, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return nil, false, fmt.Errorf("cannot create temporary cache file: %v", err)
	}
	defer outf.Cancel()

	// copy and hash at the same time
	h := c.hash.New()
	tr := io.TeeReader(inf, h)
	if _, err := io.Copy(outf, tr); err != nil {
		return nil, false, fmt.Errorf("cannot copy trusted asset to cache: %v", err)
	}
	hashStr := hex.EncodeToString(h.Sum(nil))
	cacheKey := c.assetKey(blName, assetName, hashStr)

	ta = &trackedAsset{
		blName: blName,
		name:   assetName,
		hash:   hashStr,
	}

	targetName := c.pathInCache(cacheKey)
	if osutil.FileExists(targetName) {
		// asset is already cached
		return ta, false, nil
	}
	// commit under a new name
	if err := outf.CommitAs(targetName); err != nil {
		return nil, false, fmt.Errorf("cannot commit file to assets cache: %v", err)
	}
	return ta, true, nil
}

func (c *trustedAssetsCache) Drop(blName, assetName, hashStr string) error {
	cacheKey := c.assetKey(blName, assetName, hashStr)
	if err := os.Remove(c.pathInCache(cacheKey)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// TrustedAssetsInstallObserverForModel returns a new trusted assets observer
// for use during installation of the run mode system, provided the device model
// supports secure boot. Otherwise, nil is returned.
func TrustedAssetsInstallObserverForModel(model *asserts.Model, gadgetDir string) (*TrustedAssetsInstallObserver, error) {
	if model.Grade() == asserts.ModelGradeUnset {
		// no need to observe updates when assets are not managed
		return nil, nil
	}
	if gadgetDir == "" {
		return nil, fmt.Errorf("internal error: gadget dir not provided")
	}

	return &TrustedAssetsInstallObserver{
		model:     model,
		cache:     newTrustedAssetsCache(dirs.SnapBootAssetsDir),
		gadgetDir: gadgetDir,
	}, nil
}

type trackedAsset struct {
	blName, name, hash string
}

// TrustedAssetsInstallObserver tracks the installation of trusted boot assets.
type TrustedAssetsInstallObserver struct {
	model          *asserts.Model
	gadgetDir      string
	cache          *trustedAssetsCache
	bl             bootloader.Bootloader
	trustedAssets  []string
	trackingAssets []*trackedAsset
}

// Observe observes the operation related to the content of a given gadget
// structure. In particular, the TrustedAssetsInstallObserver tracks writing of
// managed boot assets, such as the bootloader binary which is measured as part
// of the secure boot.
//
// Implements gadget.ContentObserver.
func (o *TrustedAssetsInstallObserver) Observe(op gadget.ContentOperation, affectedStruct *gadget.LaidOutStructure, root, realSource, relativeTarget string) (bool, error) {
	if affectedStruct.Role != gadget.SystemBoot {
		// only care about system-boot
		return true, nil
	}

	if o.bl == nil {
		bl, err := bootloader.ForGadget(o.gadgetDir, root, &bootloader.Options{NoSlashBoot: true})
		if err != nil {
			return false, fmt.Errorf("cannot find bootloader: %v", err)
		}
		tbl, ok := bl.(bootloader.TrustedAssetsBootloader)
		if !ok {
			return true, nil
		}
		trustedAssets, err := tbl.TrustedAssets()
		if err != nil {
			return false, fmt.Errorf("cannot list %q bootloader trusted assets: %v", bl.Name(), err)
		}
		o.trustedAssets = trustedAssets
		o.bl = bl
	}
	if len(o.trustedAssets) == 0 || !strutil.ListContains(o.trustedAssets, relativeTarget) {
		// not one of the trusted assets
		return true, nil
	}
	ta, added, err := o.cache.Add(realSource, o.bl.Name(), filepath.Base(relativeTarget))
	if err != nil {
		return false, err
	}
	if added {
		o.trackingAssets = append(o.trackingAssets, ta)
	}
	return true, nil
}

func (o *TrustedAssetsInstallObserver) currentTrustedBootAssetsMap() bootAssetsMap {
	bm := bootAssetsMap{}
	for _, tracked := range o.trackingAssets {
		bm[tracked.name] = append(bm[tracked.name], tracked.hash)
	}
	return bm
}

// TrustedAssetsUpdateObserverForModel returns a new trusted assets observer for
// tracking changes to the measured boot assets during gadget updates, provided
// the device model supports secure boot. Otherwise, nil is returned.
func TrustedAssetsUpdateObserverForModel(model *asserts.Model) *TrustedAssetsUpdateObserver {
	if model.Grade() == asserts.ModelGradeUnset {
		// no need to observe updates when assets are not managed
		return nil
	}

	return &TrustedAssetsUpdateObserver{}
}

// TrustedAssetsUpdateObserver tracks the updates of trusted boot assets and
// attempts to reseal when needed.
type TrustedAssetsUpdateObserver struct{}

// Observe observes the operation related to the update or rollback of the
// content of a given gadget structure. In particular, the
// TrustedAssetsUpdateObserver tracks updates of managed boot assets, such as
// the bootloader binary which is measured as part of the secure boot.
//
// Implements gadget.ContentUpdateObserver.
func (o *TrustedAssetsUpdateObserver) Observe(op gadget.ContentOperation, affectedStruct *gadget.LaidOutStructure, root, realSource, relativeTarget string) (bool, error) {
	// TODO:UC20:
	// steps on write action:
	// - copy new asset to assets cache
	// - update modeeenv
	// steps on rollback action:
	// - drop file from cache if no longer referenced
	// - update modeenv
	return true, nil
}

// BeforeWrite is called when the update process has been staged for execution.
func (o *TrustedAssetsUpdateObserver) BeforeWrite() error {
	// TODO:UC20:
	// - reseal with a given state of modeenv
	return nil
}

// Canceled is called when the update has been canceled, or if changes
// were written and the update has been reverted.
func (o *TrustedAssetsUpdateObserver) Canceled() error {
	// TODO:UC20:
	// - drop unused assets and update modeenv if needed
	// - reseal with a given state of modeenv
	return nil
}
