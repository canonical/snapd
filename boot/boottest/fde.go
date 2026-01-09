package boottest

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

// MockAssetsCache mocks the listed assets in the boot assets cache by creating
// an empty file for each.
func MockAssetsCache(c *C, rootdir, bootloaderName string, cachedAssets []string) {
	p := filepath.Join(dirs.SnapBootAssetsDirUnder(rootdir), bootloaderName)
	err := os.MkdirAll(p, 0o755)
	c.Assert(err, IsNil)
	for _, cachedAsset := range cachedAssets {
		err = os.WriteFile(filepath.Join(p, cachedAsset), nil, 0o644)
		c.Assert(err, IsNil)
	}
}

// MockNamedKernelSeedSnap creates a seed snap representation for a kernel snap
// with a given name and revision.
func MockNamedKernelSeedSnap(rev snap.Revision, name string) *seed.Snap {
	revAsString := rev.String()
	if rev.Unset() {
		revAsString = "unset"
	}
	return &seed.Snap{
		Path: fmt.Sprintf("/var/lib/snapd/seed/snaps/%v_%v.snap", name, revAsString),
		SideInfo: &snap.SideInfo{
			RealName: name,
			Revision: rev,
		},
		EssentialType: snap.TypeKernel,
	}
}

// MockGadgetSeedSnap creates a seed snap representation of a gadget snap with
// given snap.yaml and a list of files. If gadget.yaml is not provided in the
// file list, a mock one, referencing the grub bootloader, is added
// automatically.
func MockGadgetSeedSnap(c *C, snapYaml string, files [][]string) *seed.Snap {
	mockGadgetYaml := `
volumes:
  volumename:
    bootloader: grub
`

	hasGadgetYaml := false
	for _, entry := range files {
		if entry[0] == "meta/gadget.yaml" {
			hasGadgetYaml = true
		}
	}
	if !hasGadgetYaml {
		files = append(files, []string{"meta/gadget.yaml", mockGadgetYaml})
	}

	gadgetSnapFile := snaptest.MakeTestSnapWithFiles(c, snapYaml, files)
	return &seed.Snap{
		Path: gadgetSnapFile,
		SideInfo: &snap.SideInfo{
			RealName: "gadget",
			Revision: snap.R(1),
		},
		EssentialType: snap.TypeGadget,
	}
}
