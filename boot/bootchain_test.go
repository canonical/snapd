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

package boot_test

import (
	"encoding/json"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/testutil"
)

type bootchainSuite struct {
	testutil.BaseTest

	rootDir string
}

var _ = Suite(&bootchainSuite{})

func (s *bootchainSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.rootDir = c.MkDir()
	s.AddCleanup(func() { dirs.SetRootDir("/") })
	dirs.SetRootDir(s.rootDir)

	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapBootAssetsDir), 0755), IsNil)
}

func (s *bootchainSuite) TestBootAssetLess(c *C) {
	for _, tc := range []struct {
		l, r *boot.BootAsset
		exp  bool
	}{
		{&boot.BootAsset{Role: "recovery"}, &boot.BootAsset{Role: "run"}, true},
		{&boot.BootAsset{Role: "run"}, &boot.BootAsset{Role: "recovery"}, false},
		{&boot.BootAsset{Name: "1"}, &boot.BootAsset{Name: "11"}, true},
		{&boot.BootAsset{Name: "11"}, &boot.BootAsset{Name: "1"}, false},
		{&boot.BootAsset{Hashes: []string{"11"}}, &boot.BootAsset{Hashes: []string{"11", "11"}}, true},
		{&boot.BootAsset{Hashes: []string{"11"}}, &boot.BootAsset{Hashes: []string{"12"}}, true},
	} {
		less := boot.BootAssetLess(tc.l, tc.r)
		c.Check(less, Equals, tc.exp, Commentf("expected %v got %v for:\nl:%v\nr:%v", tc.exp, less, tc.l, tc.r))
	}
}

func (s *bootchainSuite) TestBootChainMarshalOnlyAssets(c *C) {
	pbNil := boot.ToPredictableBootChain(nil)
	c.Check(pbNil, IsNil)

	bc := &boot.BootChain{
		AssetChain: []boot.BootAsset{
			{Role: bootloader.RoleRecovery, Name: "shim", Hashes: []string{"b"}},
			{Role: bootloader.RoleRecovery, Name: "loader", Hashes: []string{"e", "d"}},
			{Role: bootloader.RoleRunMode, Name: "loader", Hashes: []string{"d", "c"}},
			{Role: bootloader.RoleRunMode, Name: "1oader", Hashes: []string{"e", "d"}},
			{Role: bootloader.RoleRunMode, Name: "0oader", Hashes: []string{"z", "x"}},
		},
	}

	predictableBc := boot.ToPredictableBootChain(bc)

	c.Check(predictableBc, DeepEquals, &boot.BootChain{
		// assets not reordered
		AssetChain: []boot.BootAsset{
			// hash lists are sorted
			{Role: bootloader.RoleRecovery, Name: "shim", Hashes: []string{"b"}},
			{Role: bootloader.RoleRecovery, Name: "loader", Hashes: []string{"e", "d"}},
			{Role: bootloader.RoleRunMode, Name: "loader", Hashes: []string{"d", "c"}},
			{Role: bootloader.RoleRunMode, Name: "1oader", Hashes: []string{"e", "d"}},
			{Role: bootloader.RoleRunMode, Name: "0oader", Hashes: []string{"z", "x"}},
		},
	})

	// already predictable, but try again
	alreadySortedBc := boot.ToPredictableBootChain(predictableBc)
	c.Check(alreadySortedBc, DeepEquals, predictableBc)

	// boot chain with 2 identical assets
	bcIdenticalAssets := &boot.BootChain{
		AssetChain: []boot.BootAsset{
			{Role: bootloader.RoleRunMode, Name: "loader", Hashes: []string{"z"}},
			{Role: bootloader.RoleRunMode, Name: "loader", Hashes: []string{"z"}},
		},
	}
	sortedBcIdentical := boot.ToPredictableBootChain(bcIdenticalAssets)
	c.Check(sortedBcIdentical, DeepEquals, bcIdenticalAssets)
}

func (s *bootchainSuite) TestBootChainMarshalFull(c *C) {
	bc := &boot.BootChain{
		BrandID:        "mybrand",
		Model:          "foo",
		Grade:          "dangerous",
		ModelSignKeyID: "my-key-id",
		// asset chain does not get sorted when marshaling
		AssetChain: []boot.BootAsset{
			// hash list will get sorted
			{Role: bootloader.RoleRecovery, Name: "shim", Hashes: []string{"b", "a"}},
			{Role: bootloader.RoleRecovery, Name: "loader", Hashes: []string{"d"}},
			{Role: bootloader.RoleRunMode, Name: "loader", Hashes: []string{"c", "d"}},
		},
		Kernel:         "pc-kernel",
		KernelRevision: "1234",
		KernelCmdlines: []string{`foo=bar baz=0x123`, `a=1`},
	}

	kernelBootFile := bootloader.NewBootFile("pc-kernel", "/foo", bootloader.RoleRecovery)
	bc.SetKernelBootFile(kernelBootFile)

	expectedPredictableBc := &boot.BootChain{
		BrandID:        "mybrand",
		Model:          "foo",
		Grade:          "dangerous",
		ModelSignKeyID: "my-key-id",
		// assets are not reordered
		AssetChain: []boot.BootAsset{
			// hash lists are sorted
			{Role: bootloader.RoleRecovery, Name: "shim", Hashes: []string{"b", "a"}},
			{Role: bootloader.RoleRecovery, Name: "loader", Hashes: []string{"d"}},
			{Role: bootloader.RoleRunMode, Name: "loader", Hashes: []string{"c", "d"}},
		},
		Kernel:         "pc-kernel",
		KernelRevision: "1234",
		KernelCmdlines: []string{`a=1`, `foo=bar baz=0x123`},
	}
	// those can't be set directly, but are copied as well
	expectedPredictableBc.SetKernelBootFile(kernelBootFile)

	predictableBc := boot.ToPredictableBootChain(bc)
	c.Check(predictableBc, DeepEquals, expectedPredictableBc)

	d, err := json.Marshal(predictableBc)
	c.Assert(err, IsNil)
	c.Check(string(d), Equals, `{"brand-id":"mybrand","model":"foo","grade":"dangerous","model-sign-key-id":"my-key-id","asset-chain":[{"role":"recovery","name":"shim","hashes":["b","a"]},{"role":"recovery","name":"loader","hashes":["d"]},{"role":"run-mode","name":"loader","hashes":["c","d"]}],"kernel":"pc-kernel","kernel-revision":"1234","kernel-cmdlines":["a=1","foo=bar baz=0x123"]}`)
	expectedOriginal := &boot.BootChain{
		BrandID:        "mybrand",
		Model:          "foo",
		Grade:          "dangerous",
		ModelSignKeyID: "my-key-id",
		AssetChain: []boot.BootAsset{
			{Role: bootloader.RoleRecovery, Name: "shim", Hashes: []string{"b", "a"}},
			{Role: bootloader.RoleRecovery, Name: "loader", Hashes: []string{"d"}},
			{Role: bootloader.RoleRunMode, Name: "loader", Hashes: []string{"c", "d"}},
		},
		Kernel:         "pc-kernel",
		KernelRevision: "1234",
		KernelCmdlines: []string{`foo=bar baz=0x123`, `a=1`},
	}
	expectedOriginal.SetKernelBootFile(kernelBootFile)
	// original structure has not been modified
	c.Check(bc, DeepEquals, expectedOriginal)
}

func (s *bootchainSuite) TestPredictableBootChainsEqualForReseal(c *C) {
	var pbNil boot.PredictableBootChains

	c.Check(boot.PredictableBootChainsEqualForReseal(pbNil, pbNil), Equals, boot.BootChainEquivalent)

	bcJustOne := []boot.BootChain{
		{
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "dangerous",
			ModelSignKeyID: "my-key-id",
			AssetChain: []boot.BootAsset{
				{Role: bootloader.RoleRecovery, Name: "shim", Hashes: []string{"b", "a"}},
				{Role: bootloader.RoleRecovery, Name: "loader", Hashes: []string{"d"}},
				{Role: bootloader.RoleRunMode, Name: "loader", Hashes: []string{"c", "d"}},
			},
			Kernel:         "pc-kernel-other",
			KernelRevision: "1234",
			KernelCmdlines: []string{`foo`},
		},
	}
	pbJustOne := boot.ToPredictableBootChains(bcJustOne)
	// equal with self
	c.Check(boot.PredictableBootChainsEqualForReseal(pbJustOne, pbJustOne), Equals, boot.BootChainEquivalent)

	// equal with nil?
	c.Check(boot.PredictableBootChainsEqualForReseal(pbJustOne, pbNil), Equals, boot.BootChainDifferent)

	bcMoreAssets := []boot.BootChain{
		{
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "dangerous",
			ModelSignKeyID: "my-key-id",
			AssetChain: []boot.BootAsset{
				{Role: bootloader.RoleRecovery, Name: "shim", Hashes: []string{"a", "b"}},
				{Role: bootloader.RoleRecovery, Name: "loader", Hashes: []string{"d"}},
			},
			Kernel:         "pc-kernel-recovery",
			KernelRevision: "1234",
			KernelCmdlines: []string{`foo`},
		}, {
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "dangerous",
			ModelSignKeyID: "my-key-id",
			AssetChain: []boot.BootAsset{
				{Role: bootloader.RoleRecovery, Name: "shim", Hashes: []string{"a", "b"}},
				{Role: bootloader.RoleRecovery, Name: "loader", Hashes: []string{"d"}},
				{Role: bootloader.RoleRunMode, Name: "loader", Hashes: []string{"c", "d"}},
			},
			Kernel:         "pc-kernel-other",
			KernelRevision: "1234",
			KernelCmdlines: []string{`foo`},
		},
	}

	pbMoreAssets := boot.ToPredictableBootChains(bcMoreAssets)

	c.Check(boot.PredictableBootChainsEqualForReseal(pbMoreAssets, pbJustOne), Equals, boot.BootChainDifferent)
	// with self
	c.Check(boot.PredictableBootChainsEqualForReseal(pbMoreAssets, pbMoreAssets), Equals, boot.BootChainEquivalent)
	// chains composed of respective elements are not equal
	c.Check(boot.PredictableBootChainsEqualForReseal(
		[]boot.BootChain{pbMoreAssets[0]},
		[]boot.BootChain{pbMoreAssets[1]}),
		Equals, boot.BootChainDifferent)

	// unrevisioned/unasserted kernels
	bcUnrevOne := []boot.BootChain{pbJustOne[0]}
	bcUnrevOne[0].KernelRevision = ""
	pbUnrevOne := boot.ToPredictableBootChains(bcUnrevOne)
	// soundness
	c.Check(boot.PredictableBootChainsEqualForReseal(pbJustOne, pbJustOne), Equals, boot.BootChainEquivalent)
	// never equal even with self because of unrevisioned
	c.Check(boot.PredictableBootChainsEqualForReseal(pbJustOne, pbUnrevOne), Equals, boot.BootChainDifferent)
	c.Check(boot.PredictableBootChainsEqualForReseal(pbUnrevOne, pbUnrevOne), Equals, boot.BootChainUnrevisioned)

	bcUnrevMoreAssets := []boot.BootChain{pbMoreAssets[0], pbMoreAssets[1]}
	bcUnrevMoreAssets[1].KernelRevision = ""
	pbUnrevMoreAssets := boot.ToPredictableBootChains(bcUnrevMoreAssets)
	// never equal even with self because of unrevisioned
	c.Check(boot.PredictableBootChainsEqualForReseal(pbUnrevMoreAssets, pbMoreAssets), Equals, boot.BootChainDifferent)
	c.Check(boot.PredictableBootChainsEqualForReseal(pbUnrevMoreAssets, pbUnrevMoreAssets), Equals, boot.BootChainUnrevisioned)
}

func (s *bootchainSuite) TestPredictableBootChainsFullMarshal(c *C) {
	// chains will be sorted
	chains := []boot.BootChain{
		{
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "signed",
			ModelSignKeyID: "my-key-id",
			AssetChain: []boot.BootAsset{
				// hashes will be sorted
				{Role: bootloader.RoleRecovery, Name: "shim", Hashes: []string{"x", "y"}},
				{Role: bootloader.RoleRecovery, Name: "loader", Hashes: []string{"c", "d"}},
				{Role: bootloader.RoleRunMode, Name: "loader", Hashes: []string{"z", "x"}},
			},
			Kernel:         "pc-kernel-other",
			KernelRevision: "2345",
			KernelCmdlines: []string{`snapd_recovery_mode=run foo`},
		}, {
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "dangerous",
			ModelSignKeyID: "my-key-id",
			AssetChain: []boot.BootAsset{
				// hashes will be sorted
				{Role: bootloader.RoleRecovery, Name: "shim", Hashes: []string{"y", "x"}},
				{Role: bootloader.RoleRecovery, Name: "loader", Hashes: []string{"c", "d"}},
				{Role: bootloader.RoleRunMode, Name: "loader", Hashes: []string{"b", "a"}},
			},
			Kernel:         "pc-kernel-other",
			KernelRevision: "1234",
			KernelCmdlines: []string{`snapd_recovery_mode=run foo`},
		}, {
			// recovery system
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "dangerous",
			ModelSignKeyID: "my-key-id",
			AssetChain: []boot.BootAsset{
				// hashes will be sorted
				{Role: bootloader.RoleRecovery, Name: "shim", Hashes: []string{"y", "x"}},
				{Role: bootloader.RoleRecovery, Name: "loader", Hashes: []string{"c", "d"}},
			},
			Kernel:         "pc-kernel-other",
			KernelRevision: "12",
			KernelCmdlines: []string{
				// will be sorted
				`snapd_recovery_mode=recover snapd_recovery_system=23 foo`,
				`snapd_recovery_mode=recover snapd_recovery_system=12 foo`,
			},
		},
	}

	predictableChains := boot.ToPredictableBootChains(chains)
	d, err := json.Marshal(predictableChains)
	c.Assert(err, IsNil)

	var data []map[string]interface{}
	err = json.Unmarshal(d, &data)
	c.Assert(err, IsNil)
	c.Check(data, DeepEquals, []map[string]interface{}{
		{
			"model":             "foo",
			"brand-id":          "mybrand",
			"grade":             "dangerous",
			"model-sign-key-id": "my-key-id",
			"kernel":            "pc-kernel-other",
			"kernel-revision":   "12",
			"kernel-cmdlines": []interface{}{
				`snapd_recovery_mode=recover snapd_recovery_system=12 foo`,
				`snapd_recovery_mode=recover snapd_recovery_system=23 foo`,
			},
			"asset-chain": []interface{}{
				map[string]interface{}{"role": "recovery", "name": "shim", "hashes": []interface{}{"y", "x"}},
				map[string]interface{}{"role": "recovery", "name": "loader", "hashes": []interface{}{"c", "d"}},
			},
		}, {
			"model":             "foo",
			"brand-id":          "mybrand",
			"grade":             "dangerous",
			"model-sign-key-id": "my-key-id",
			"kernel":            "pc-kernel-other",
			"kernel-revision":   "1234",
			"kernel-cmdlines":   []interface{}{"snapd_recovery_mode=run foo"},
			"asset-chain": []interface{}{
				map[string]interface{}{"role": "recovery", "name": "shim", "hashes": []interface{}{"y", "x"}},
				map[string]interface{}{"role": "recovery", "name": "loader", "hashes": []interface{}{"c", "d"}},
				map[string]interface{}{"role": "run-mode", "name": "loader", "hashes": []interface{}{"b", "a"}},
			},
		}, {
			"model":             "foo",
			"brand-id":          "mybrand",
			"grade":             "signed",
			"model-sign-key-id": "my-key-id",
			"kernel":            "pc-kernel-other",
			"kernel-revision":   "2345",
			"kernel-cmdlines":   []interface{}{"snapd_recovery_mode=run foo"},
			"asset-chain": []interface{}{
				map[string]interface{}{"role": "recovery", "name": "shim", "hashes": []interface{}{"x", "y"}},
				map[string]interface{}{"role": "recovery", "name": "loader", "hashes": []interface{}{"c", "d"}},
				map[string]interface{}{"role": "run-mode", "name": "loader", "hashes": []interface{}{"z", "x"}},
			},
		},
	})
}

func (s *bootchainSuite) TestPredictableBootChainsFields(c *C) {
	chainsNil := boot.ToPredictableBootChains(nil)
	c.Check(chainsNil, IsNil)

	justOne := []boot.BootChain{
		{
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "signed",
			ModelSignKeyID: "my-key-id",
			Kernel:         "pc-kernel-other",
			KernelRevision: "2345",
			KernelCmdlines: []string{`foo`},
		},
	}
	predictableJustOne := boot.ToPredictableBootChains(justOne)
	c.Check(predictableJustOne, DeepEquals, boot.PredictableBootChains(justOne))

	chainsGrade := []boot.BootChain{
		{
			Grade: "signed",
		}, {
			Grade: "dangerous",
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsGrade), DeepEquals, boot.PredictableBootChains{
		{
			Grade: "dangerous",
		}, {
			Grade: "signed",
		},
	})

	chainsKernel := []boot.BootChain{
		{
			Grade:  "dangerous",
			Kernel: "foo",
		}, {
			Grade:  "dangerous",
			Kernel: "bar",
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsKernel), DeepEquals, boot.PredictableBootChains{
		{
			Grade:  "dangerous",
			Kernel: "bar",
		}, {
			Grade:  "dangerous",
			Kernel: "foo",
		},
	})

	chainsKernelRevision := []boot.BootChain{
		{
			Kernel:         "foo",
			KernelRevision: "9",
		}, {
			Kernel:         "foo",
			KernelRevision: "21",
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsKernelRevision), DeepEquals, boot.PredictableBootChains{
		{
			Kernel:         "foo",
			KernelRevision: "21",
		}, {
			Kernel:         "foo",
			KernelRevision: "9",
		},
	})

	chainsCmdline := []boot.BootChain{
		{
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`a`},
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsCmdline), DeepEquals, boot.PredictableBootChains{
		{
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`a`},
		}, {
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	})

	chainsModel := []boot.BootChain{
		{
			Model:          "fridge",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsModel), DeepEquals, boot.PredictableBootChains{
		{
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			Model:          "fridge",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	})

	chainsBrand := []boot.BootChain{
		{
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			BrandID:        "acme",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsBrand), DeepEquals, boot.PredictableBootChains{
		{
			BrandID:        "acme",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	})

	chainsKeyID := []boot.BootChain{
		{
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
			ModelSignKeyID: "key-2",
		}, {
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
			ModelSignKeyID: "key-1",
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsKeyID), DeepEquals, boot.PredictableBootChains{
		{
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
			ModelSignKeyID: "key-1",
		}, {
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
			ModelSignKeyID: "key-2",
		},
	})

	chainsAssets := []boot.BootChain{
		{
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			ModelSignKeyID: "key-1",
			AssetChain: []boot.BootAsset{
				// will be sorted
				{Hashes: []string{"b", "a"}},
			},
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			ModelSignKeyID: "key-1",
			AssetChain: []boot.BootAsset{
				{Hashes: []string{"b"}},
			},
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsAssets), DeepEquals, boot.PredictableBootChains{
		{
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			ModelSignKeyID: "key-1",
			AssetChain: []boot.BootAsset{
				{Hashes: []string{"b"}},
			},
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			ModelSignKeyID: "key-1",
			AssetChain: []boot.BootAsset{
				{Hashes: []string{"b", "a"}},
			},
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	})

	chainsFewerAssets := []boot.BootChain{
		{
			AssetChain: []boot.BootAsset{
				{Hashes: []string{"b", "a"}},
				{Hashes: []string{"c", "d"}},
			},
		}, {
			AssetChain: []boot.BootAsset{
				{Hashes: []string{"b"}},
			},
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsFewerAssets), DeepEquals, boot.PredictableBootChains{
		{
			AssetChain: []boot.BootAsset{
				{Hashes: []string{"b"}},
			},
		}, {
			AssetChain: []boot.BootAsset{
				{Hashes: []string{"b", "a"}},
				{Hashes: []string{"c", "d"}},
			},
		},
	})

	// not confused if 2 chains are identical
	chainsIdenticalAssets := []boot.BootChain{
		{
			BrandID:        "foo",
			Model:          "box",
			ModelSignKeyID: "key-1",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"a", "b"}},
				{Name: "asset", Hashes: []string{"a", "b"}},
			},
			Grade:          "dangerous",
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		}, {
			BrandID:        "foo",
			Model:          "box",
			Grade:          "dangerous",
			ModelSignKeyID: "key-1",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"a", "b"}},
				{Name: "asset", Hashes: []string{"a", "b"}},
			},
			Kernel:         "foo",
			KernelCmdlines: []string{`panic=1`},
		},
	}
	c.Check(boot.ToPredictableBootChains(chainsIdenticalAssets), DeepEquals, boot.PredictableBootChains(chainsIdenticalAssets))
}

func (s *bootchainSuite) TestPredictableBootChainsSortOrder(c *C) {
	// check that sort order is model info, assets, kernel, kernel cmdline

	chains := []boot.BootChain{
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1", "cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1", "cm=2"},
		},
	}
	predictable := boot.ToPredictableBootChains(chains)
	c.Check(predictable, DeepEquals, boot.PredictableBootChains{
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1", "cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "a",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1", "cm=2"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"x"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k1",
			KernelCmdlines: []string{"cm=2"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=1"},
		},
		{
			Model: "b",
			AssetChain: []boot.BootAsset{
				{Name: "asset", Hashes: []string{"y"}},
			},
			Kernel:         "k2",
			KernelCmdlines: []string{"cm=2"},
		},
	})
}

func printChain(c *C, chain *secboot.LoadChain, prefix string) {
	c.Logf("%v %v", prefix, chain.BootFile)
	for _, n := range chain.Next {
		printChain(c, n, prefix+"-")
	}
}

// cPath returns a path under boot assets cache directory
func cPath(p string) string {
	return filepath.Join(dirs.SnapBootAssetsDir, p)
}

// nbf is bootloader.NewBootFile but shorter
var nbf = bootloader.NewBootFile

func (s *bootchainSuite) TestBootAssetsToLoadChainTrivialKernel(c *C) {
	kbl := bootloader.NewBootFile("pc-kernel", "kernel.efi", bootloader.RoleRunMode)

	chains, err := boot.BootAssetsToLoadChains(nil, kbl, nil, false)
	c.Assert(err, IsNil)

	c.Check(chains, DeepEquals, []*secboot.LoadChain{
		secboot.NewLoadChain(nbf("pc-kernel", "kernel.efi", bootloader.RoleRunMode)),
	})
}

func (s *bootchainSuite) TestBootAssetsToLoadChainErr(c *C) {
	kbl := bootloader.NewBootFile("pc-kernel", "kernel.efi", bootloader.RoleRunMode)

	assets := []boot.BootAsset{
		{Name: "shim", Hashes: []string{"hash0"}, Role: bootloader.RoleRecovery},
		{Name: "loader-recovery", Hashes: []string{"hash0"}, Role: bootloader.RoleRecovery},
		{Name: "loader-run", Hashes: []string{"hash0"}, Role: bootloader.RoleRunMode},
	}

	blNames := map[bootloader.Role]string{
		bootloader.RoleRecovery: "recovery-bl",
		// missing bootloader name for role "run-mode"
	}
	// fails when probing the shim asset in the cache
	chains, err := boot.BootAssetsToLoadChains(assets, kbl, blNames, false)
	c.Assert(err, ErrorMatches, "file .*/recovery-bl/shim-hash0 not found in boot assets cache")
	c.Check(chains, IsNil)
	// make it work now
	c.Assert(os.MkdirAll(filepath.Dir(cPath("recovery-bl/shim-hash0")), 0755), IsNil)
	c.Assert(os.WriteFile(cPath("recovery-bl/shim-hash0"), nil, 0644), IsNil)

	// nested error bubbled up
	chains, err = boot.BootAssetsToLoadChains(assets, kbl, blNames, false)
	c.Assert(err, ErrorMatches, "file .*/recovery-bl/loader-recovery-hash0 not found in boot assets cache")
	c.Check(chains, IsNil)
	// again, make it work
	c.Assert(os.MkdirAll(filepath.Dir(cPath("recovery-bl/loader-recovery-hash0")), 0755), IsNil)
	c.Assert(os.WriteFile(cPath("recovery-bl/loader-recovery-hash0"), nil, 0644), IsNil)

	// fails on missing bootloader name for role "run-mode"
	chains, err = boot.BootAssetsToLoadChains(assets, kbl, blNames, false)
	c.Assert(err, ErrorMatches, `internal error: no bootloader name for boot asset role "run-mode"`)
	c.Check(chains, IsNil)
}

func (s *bootchainSuite) TestBootAssetsToLoadChainSimpleChain(c *C) {
	kbl := bootloader.NewBootFile("pc-kernel", "kernel.efi", bootloader.RoleRunMode)

	assets := []boot.BootAsset{
		{Name: "shim", Hashes: []string{"hash0"}, Role: bootloader.RoleRecovery},
		{Name: "loader-recovery", Hashes: []string{"hash0"}, Role: bootloader.RoleRecovery},
		{Name: "loader-run", Hashes: []string{"hash0"}, Role: bootloader.RoleRunMode},
	}

	// mock relevant files in cache
	for _, name := range []string{
		"recovery-bl/shim-hash0",
		"recovery-bl/loader-recovery-hash0",
		"run-bl/loader-run-hash0",
	} {
		p := filepath.Join(dirs.SnapBootAssetsDir, name)
		c.Assert(os.MkdirAll(filepath.Dir(p), 0755), IsNil)
		c.Assert(os.WriteFile(p, nil, 0644), IsNil)
	}

	blNames := map[bootloader.Role]string{
		bootloader.RoleRecovery: "recovery-bl",
		bootloader.RoleRunMode:  "run-bl",
	}

	chains, err := boot.BootAssetsToLoadChains(assets, kbl, blNames, false)
	c.Assert(err, IsNil)

	c.Logf("got:")
	for _, ch := range chains {
		printChain(c, ch, "-")
	}

	expected := []*secboot.LoadChain{
		secboot.NewLoadChain(nbf("", cPath("recovery-bl/shim-hash0"), bootloader.RoleRecovery),
			secboot.NewLoadChain(nbf("", cPath("recovery-bl/loader-recovery-hash0"), bootloader.RoleRecovery),
				secboot.NewLoadChain(nbf("", cPath("run-bl/loader-run-hash0"), bootloader.RoleRunMode),
					secboot.NewLoadChain(nbf("pc-kernel", "kernel.efi", bootloader.RoleRunMode))))),
	}
	c.Check(chains, DeepEquals, expected)
}

func (s *bootchainSuite) TestBootAssetsToLoadChainWithAlternativeChains(c *C) {
	kbl := bootloader.NewBootFile("pc-kernel", "kernel.efi", bootloader.RoleRunMode)

	assets := []boot.BootAsset{
		{Name: "shim", Hashes: []string{"hash0", "hash1"}, Role: bootloader.RoleRecovery},
		{Name: "loader-recovery", Hashes: []string{"hash0", "hash1"}, Role: bootloader.RoleRecovery},
		{Name: "loader-run", Hashes: []string{"hash0", "hash1"}, Role: bootloader.RoleRunMode},
	}

	// mock relevant files in cache
	mockAssetsCache(c, s.rootDir, "recovery-bl", []string{
		"shim-hash0",
		"shim-hash1",
		"loader-recovery-hash0",
		"loader-recovery-hash1",
	})
	mockAssetsCache(c, s.rootDir, "run-bl", []string{
		"loader-run-hash0",
		"loader-run-hash1",
	})

	blNames := map[bootloader.Role]string{
		bootloader.RoleRecovery: "recovery-bl",
		bootloader.RoleRunMode:  "run-bl",
	}
	chains, err := boot.BootAssetsToLoadChains(assets, kbl, blNames, false)
	c.Assert(err, IsNil)

	c.Logf("got:")
	for _, ch := range chains {
		printChain(c, ch, "-")
	}

	expected := []*secboot.LoadChain{
		secboot.NewLoadChain(nbf("", cPath("recovery-bl/shim-hash0"), bootloader.RoleRecovery),
			secboot.NewLoadChain(nbf("", cPath("recovery-bl/loader-recovery-hash0"), bootloader.RoleRecovery),
				secboot.NewLoadChain(nbf("", cPath("run-bl/loader-run-hash0"), bootloader.RoleRunMode),
					secboot.NewLoadChain(nbf("pc-kernel", "kernel.efi", bootloader.RoleRunMode))),
				secboot.NewLoadChain(nbf("", cPath("run-bl/loader-run-hash1"), bootloader.RoleRunMode),
					secboot.NewLoadChain(nbf("pc-kernel", "kernel.efi", bootloader.RoleRunMode)))),
			secboot.NewLoadChain(nbf("", cPath("recovery-bl/loader-recovery-hash1"), bootloader.RoleRecovery),
				secboot.NewLoadChain(nbf("", cPath("run-bl/loader-run-hash1"), bootloader.RoleRunMode),
					secboot.NewLoadChain(nbf("pc-kernel", "kernel.efi", bootloader.RoleRunMode))))),
		secboot.NewLoadChain(nbf("", cPath("recovery-bl/shim-hash1"), bootloader.RoleRecovery),
			secboot.NewLoadChain(nbf("", cPath("recovery-bl/loader-recovery-hash1"), bootloader.RoleRecovery),
				secboot.NewLoadChain(nbf("", cPath("run-bl/loader-run-hash1"), bootloader.RoleRunMode),
					secboot.NewLoadChain(nbf("pc-kernel", "kernel.efi", bootloader.RoleRunMode))))),
	}
	c.Check(chains, DeepEquals, expected)
}

func (s *sealSuite) TestReadWriteBootChains(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("the test cannot be run by the root user")
	}

	chains := []boot.BootChain{
		{
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "signed",
			ModelSignKeyID: "my-key-id",
			AssetChain: []boot.BootAsset{
				// hashes will be sorted
				{Role: bootloader.RoleRecovery, Name: "shim", Hashes: []string{"x", "y"}},
				{Role: bootloader.RoleRecovery, Name: "loader", Hashes: []string{"c", "d"}},
				{Role: bootloader.RoleRunMode, Name: "loader", Hashes: []string{"z", "x"}},
			},
			Kernel:         "pc-kernel-other",
			KernelRevision: "2345",
			KernelCmdlines: []string{`snapd_recovery_mode=run foo`},
		}, {
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "dangerous",
			ModelSignKeyID: "my-key-id",
			AssetChain: []boot.BootAsset{
				// hashes will be sorted
				{Role: bootloader.RoleRecovery, Name: "shim", Hashes: []string{"y", "x"}},
				{Role: bootloader.RoleRecovery, Name: "loader", Hashes: []string{"c", "d"}},
			},
			Kernel:         "pc-kernel-recovery",
			KernelRevision: "1234",
			KernelCmdlines: []string{`snapd_recovery_mode=recover foo`},
		},
	}

	pbc := boot.ToPredictableBootChains(chains)

	rootdir := c.MkDir()

	expected := `{"reseal-count":0,"boot-chains":[{"brand-id":"mybrand","model":"foo","grade":"dangerous","model-sign-key-id":"my-key-id","asset-chain":[{"role":"recovery","name":"shim","hashes":["y","x"]},{"role":"recovery","name":"loader","hashes":["c","d"]}],"kernel":"pc-kernel-recovery","kernel-revision":"1234","kernel-cmdlines":["snapd_recovery_mode=recover foo"]},{"brand-id":"mybrand","model":"foo","grade":"signed","model-sign-key-id":"my-key-id","asset-chain":[{"role":"recovery","name":"shim","hashes":["x","y"]},{"role":"recovery","name":"loader","hashes":["c","d"]},{"role":"run-mode","name":"loader","hashes":["z","x"]}],"kernel":"pc-kernel-other","kernel-revision":"2345","kernel-cmdlines":["snapd_recovery_mode=run foo"]}]}
`
	// creates a complete tree and writes a file
	err := boot.WriteBootChains(pbc, filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), 0)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), testutil.FileEquals, expected)

	fi, err := os.Stat(filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"))
	c.Assert(err, IsNil)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0600))

	loaded, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"))
	c.Assert(err, IsNil)
	c.Check(loaded, DeepEquals, pbc)
	c.Check(cnt, Equals, 0)
	// boot chains should be same for reseal purpose
	c.Check(boot.PredictableBootChainsEqualForReseal(pbc, loaded), Equals, boot.BootChainEquivalent)

	// write them again with count > 0
	err = boot.WriteBootChains(pbc, filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), 99)
	c.Assert(err, IsNil)

	_, cnt, err = boot.ReadBootChains(filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"))
	c.Assert(err, IsNil)
	c.Check(cnt, Equals, 99)

	// make device/fde directory read only so that writing fails
	otherRootdir := c.MkDir()
	c.Assert(os.MkdirAll(dirs.SnapFDEDirUnder(otherRootdir), 0755), IsNil)
	c.Assert(os.Chmod(dirs.SnapFDEDirUnder(otherRootdir), 0000), IsNil)
	defer os.Chmod(dirs.SnapFDEDirUnder(otherRootdir), 0755)

	err = boot.WriteBootChains(pbc, filepath.Join(dirs.SnapFDEDirUnder(otherRootdir), "boot-chains"), 0)
	c.Assert(err, ErrorMatches, `cannot create a temporary boot chains file: open .*/boot-chains\.[a-zA-Z0-9]+~: permission denied`)

	// make the original file non readable
	c.Assert(os.Chmod(filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), 0000), IsNil)
	defer os.Chmod(filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), 0755)
	loaded, _, err = boot.ReadBootChains(filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"))
	c.Assert(err, ErrorMatches, "cannot open existing boot chains data file: open .*/boot-chains: permission denied")
	c.Check(loaded, IsNil)

	// loading from a file that does not exist yields a nil boot chain
	// and 0 count
	loaded, cnt, err = boot.ReadBootChains("does-not-exist")
	c.Assert(err, IsNil)
	c.Check(loaded, IsNil)
	c.Check(cnt, Equals, 0)
}

func (s *bootchainSuite) TestModelForSealing(c *C) {
	bc := boot.BootChain{
		BrandID:        "my-brand",
		Model:          "my-model",
		Grade:          "signed",
		ModelSignKeyID: "my-key-id",
	}

	modelForSealing := bc.SecbootModelForSealing()
	c.Check(modelForSealing.Model(), Equals, "my-model")
	c.Check(modelForSealing.BrandID(), Equals, "my-brand")
	c.Check(modelForSealing.Classic(), Equals, false)
	c.Check(modelForSealing.Grade(), Equals, asserts.ModelGrade("signed"))
	c.Check(modelForSealing.SignKeyID(), Equals, "my-key-id")
	c.Check(modelForSealing.Series(), Equals, "16")
	c.Check(boot.ModelUniqueID(modelForSealing), Equals, "my-brand/my-model,signed,my-key-id")

}

func (s *bootchainSuite) TestClassicModelForSealing(c *C) {
	bc := boot.BootChain{
		BrandID:        "my-brand",
		Model:          "my-model",
		Classic:        true,
		Grade:          "signed",
		ModelSignKeyID: "my-key-id",
	}

	modelForSealing := bc.SecbootModelForSealing()
	c.Check(modelForSealing.Model(), Equals, "my-model")
	c.Check(modelForSealing.BrandID(), Equals, "my-brand")
	c.Check(modelForSealing.Classic(), Equals, true)
	c.Check(boot.ModelUniqueID(modelForSealing), Equals, "my-brand/my-model,signed,my-key-id")
}
