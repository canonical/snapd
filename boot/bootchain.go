// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020-2022 Canonical Ltd
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
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
)

// TODO:UC20 add a doc comment when this is stabilized
type bootChain struct {
	BrandID        string             `json:"brand-id"`
	Model          string             `json:"model"`
	Classic        bool               `json:"classic,omitempty"`
	Grade          asserts.ModelGrade `json:"grade"`
	ModelSignKeyID string             `json:"model-sign-key-id"`
	AssetChain     []bootAsset        `json:"asset-chain"`
	Kernel         string             `json:"kernel"`
	// KernelRevision is the revision of the kernel snap. It is empty if
	// kernel is unasserted, in which case always reseal.
	KernelRevision string   `json:"kernel-revision"`
	KernelCmdlines []string `json:"kernel-cmdlines"`

	kernelBootFile bootloader.BootFile
}

func (b *bootChain) modelForSealing() *modelForSealing {
	return &modelForSealing{
		brandID:        b.BrandID,
		model:          b.Model,
		classic:        b.Classic,
		grade:          b.Grade,
		modelSignKeyID: b.ModelSignKeyID,
	}
}

// TODO:UC20 add a doc comment when this is stabilized
type bootAsset struct {
	Role   bootloader.Role `json:"role"`
	Name   string          `json:"name"`
	Hashes []string        `json:"hashes"`
}

func bootAssetLess(b, other *bootAsset) bool {
	byRole := b.Role < other.Role
	byName := b.Name < other.Name
	// sort order: role -> name -> hash list (len -> lexical)
	if b.Role != other.Role {
		return byRole
	}
	if b.Name != other.Name {
		return byName
	}
	return stringListsLess(b.Hashes, other.Hashes)
}

func stringListsEqual(sl1, sl2 []string) bool {
	if len(sl1) != len(sl2) {
		return false
	}
	for i := range sl1 {
		if sl1[i] != sl2[i] {
			return false
		}
	}
	return true
}

func stringListsLess(sl1, sl2 []string) bool {
	if len(sl1) != len(sl2) {
		return len(sl1) < len(sl2)
	}
	for idx := range sl1 {
		if sl1[idx] < sl2[idx] {
			return true
		}
	}
	return false
}

func toPredictableBootChain(b *bootChain) *bootChain {
	if b == nil {
		return nil
	}
	newB := *b
	// AssetChain is sorted list (by boot order) of sorted list (old to new asset).
	// So it is already predictable and we can keep it the way it is.

	// However we still need to sort kernel KernelCmdlines
	if b.KernelCmdlines != nil {
		newB.KernelCmdlines = make([]string, len(b.KernelCmdlines))
		copy(newB.KernelCmdlines, b.KernelCmdlines)
		sort.Strings(newB.KernelCmdlines)
	}
	return &newB
}

func predictableBootAssetsEqual(b1, b2 []bootAsset) bool {
	b1JSON, err := json.Marshal(b1)
	if err != nil {
		return false
	}
	b2JSON, err := json.Marshal(b2)
	if err != nil {
		return false
	}
	return bytes.Equal(b1JSON, b2JSON)
}

func predictableBootAssetsLess(b1, b2 []bootAsset) bool {
	if len(b1) != len(b2) {
		return len(b1) < len(b2)
	}
	for i := range b1 {
		if bootAssetLess(&b1[i], &b2[i]) {
			return true
		}
	}
	return false
}

type byBootChainOrder []bootChain

func (b byBootChainOrder) Len() int      { return len(b) }
func (b byBootChainOrder) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b byBootChainOrder) Less(i, j int) bool {
	// sort by model info
	if b[i].BrandID != b[j].BrandID {
		return b[i].BrandID < b[j].BrandID
	}
	if b[i].Model != b[j].Model {
		return b[i].Model < b[j].Model
	}
	if b[i].Grade != b[j].Grade {
		return b[i].Grade < b[j].Grade
	}
	if b[i].ModelSignKeyID != b[j].ModelSignKeyID {
		return b[i].ModelSignKeyID < b[j].ModelSignKeyID
	}
	// then boot assets
	if !predictableBootAssetsEqual(b[i].AssetChain, b[j].AssetChain) {
		return predictableBootAssetsLess(b[i].AssetChain, b[j].AssetChain)
	}
	// then kernel
	if b[i].Kernel != b[j].Kernel {
		return b[i].Kernel < b[j].Kernel
	}
	if b[i].KernelRevision != b[j].KernelRevision {
		return b[i].KernelRevision < b[j].KernelRevision
	}
	// and last kernel command lines
	if !stringListsEqual(b[i].KernelCmdlines, b[j].KernelCmdlines) {
		return stringListsLess(b[i].KernelCmdlines, b[j].KernelCmdlines)
	}
	return false
}

type predictableBootChains []bootChain

// hasUnrevisionedKernels returns true if any of the chains have an
// unrevisioned kernel. Revisions will not be set for unasserted
// kernels.
func (pbc predictableBootChains) hasUnrevisionedKernels() bool {
	for i := range pbc {
		if pbc[i].KernelRevision == "" {
			return true
		}
	}
	return false
}

func toPredictableBootChains(chains []bootChain) predictableBootChains {
	if chains == nil {
		return nil
	}
	predictableChains := make([]bootChain, len(chains))
	for i := range chains {
		predictableChains[i] = *toPredictableBootChain(&chains[i])
	}
	sort.Sort(byBootChainOrder(predictableChains))
	return predictableChains
}

type bootChainEquivalence int

const (
	bootChainEquivalent   bootChainEquivalence = 0
	bootChainDifferent    bootChainEquivalence = 1
	bootChainUnrevisioned bootChainEquivalence = -1
)

// predictableBootChainsEqualForReseal returns bootChainEquivalent
// when boot chains are equivalent for reseal. If the boot chains
// are clearly different it returns bootChainDifferent.
// If it would return bootChainEquivalent but the chains contain
// unrevisioned kernels it will return bootChainUnrevisioned.
func predictableBootChainsEqualForReseal(pb1, pb2 predictableBootChains) bootChainEquivalence {
	pb1JSON, err := json.Marshal(pb1)
	if err != nil {
		return bootChainDifferent
	}
	pb2JSON, err := json.Marshal(pb2)
	if err != nil {
		return bootChainDifferent
	}
	if bytes.Equal(pb1JSON, pb2JSON) {
		if pb1.hasUnrevisionedKernels() {
			return bootChainUnrevisioned
		}
		return bootChainEquivalent
	}
	return bootChainDifferent
}

// bootAssetsToLoadChains generates a list of load chains covering given boot
// assets sequence. At the end of each chain, adds an entry for the kernel boot
// file.
// We do not calculate some boot chains because they are impossible as
// when we update assets we write first the binaries that are used
// later, that is, if updating both shim and grub, the new grub is
// copied first to the disk, so booting from the new shim to the old
// grub is not possible. This is controlled by expectNew, that tells
// us that the previous step in the chain is from a new asset.
func bootAssetsToLoadChains(assets []bootAsset, kernelBootFile bootloader.BootFile, roleToBlName map[bootloader.Role]string, expectNew bool) ([]*secboot.LoadChain, error) {
	// kernel is added after all the assets
	addKernelBootFile := len(assets) == 0
	if addKernelBootFile {
		return []*secboot.LoadChain{secboot.NewLoadChain(kernelBootFile)}, nil
	}

	thisAsset := assets[0]
	blName := roleToBlName[thisAsset.Role]
	if blName == "" {
		return nil, fmt.Errorf("internal error: no bootloader name for boot asset role %q", thisAsset.Role)
	}
	var chains []*secboot.LoadChain

	for i, hash := range thisAsset.Hashes {
		// There should be 1 or 2 assets, and their position has a meaning.
		// See TrustedAssetsUpdateObserver.observeUpdate
		if i == 0 {
			// i == 0 means currently installed asset.
			// We do not expect this asset to be used as
			// we have new assets earlier in the chain
			if len(thisAsset.Hashes) == 2 && expectNew {
				continue
			}
		} else if i == 1 {
			// i == 1 means new asset
		} else {
			// If there is a second asset, it is the next asset to be installed
			return nil, fmt.Errorf("internal error: did not expect more than 2 hashes for %s", thisAsset.Name)
		}

		var bf bootloader.BootFile
		var next []*secboot.LoadChain
		var err error

		p := filepath.Join(
			dirs.SnapBootAssetsDir,
			trustedAssetCacheRelPath(blName, thisAsset.Name, hash))
		if !osutil.FileExists(p) {
			return nil, fmt.Errorf("file %s not found in boot assets cache", p)
		}
		bf = bootloader.NewBootFile(
			"", // asset comes from the filesystem, not a snap
			p,
			thisAsset.Role,
		)
		next, err = bootAssetsToLoadChains(assets[1:], kernelBootFile, roleToBlName, expectNew || i == 1)
		if err != nil {
			return nil, err
		}
		chains = append(chains, secboot.NewLoadChain(bf, next...))
	}
	return chains, nil
}

// predictableBootChainsWrapperForStorage wraps the boot chains so
// that we do not store the arrays directly as JSON and we can add
// other information
type predictableBootChainsWrapperForStorage struct {
	ResealCount int                   `json:"reseal-count"`
	BootChains  predictableBootChains `json:"boot-chains"`
}

func readBootChains(path string) (pbc predictableBootChains, resealCount int, err error) {
	inf, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("cannot open existing boot chains data file: %v", err)
	}
	defer inf.Close()
	var wrapped predictableBootChainsWrapperForStorage
	if err := json.NewDecoder(inf).Decode(&wrapped); err != nil {
		return nil, 0, fmt.Errorf("cannot read boot chains data: %v", err)
	}
	return wrapped.BootChains, wrapped.ResealCount, nil
}

func writeBootChains(pbc predictableBootChains, path string, resealCount int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("cannot create device fde state directory: %v", err)
	}
	outf, err := osutil.NewAtomicFile(path, 0600, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return fmt.Errorf("cannot create a temporary boot chains file: %v", err)
	}
	// becomes noop when the file is committed
	defer outf.Cancel()

	wrapped := predictableBootChainsWrapperForStorage{
		ResealCount: resealCount,
		BootChains:  pbc,
	}
	if err := json.NewEncoder(outf).Encode(wrapped); err != nil {
		return fmt.Errorf("cannot write boot chains data: %v", err)
	}
	return outf.Commit()
}
