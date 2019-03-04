package apparmor

import (
	"os"
	"path/filepath"
)

// Legacy represents apparmor user-space version up until 1.12
//
// Legacy apparmor uses the following structure:
//
//   /etc/apparmor.d/* (just files): source profiles.
//   /etc/apparmor.d/tunables/*: definitions included by source profiles.
//   /etc/apparmor.d/abstractions/*: definitions included by source profiles.
//   /etc/apparmor.d/cache/*: binary profiles.
//
// In addition some locations are only used by snapd:
//
//   /var/lib/snapd/apparmor/profiles: source profiles (snapd specific)
//   /var/cache/apparmor/*: binary profiles (snapd specific)
//
// Source profiles are typically stored as files in /etc/apparmor.d/.
// The file name is the absolute path of the executable with forward slashes
// replaced with dots. For example the file /etc/apparmor.d/usr.bin.man
// contains a profile that attaches to /usr/bin/man. In addition the same file
// defines additional profiles man_groff and man_filter, that various
// executables started from /usr/bin/man assume.
//
// Source profiles routinely include definitions from
// /etc/apparmor.d/abstractions as well as tunables that can be adjusted by
// local administrator, from /etc/apparmor.d/tunables.
//
// Once compiled, binary profiles are typically cached as files in
// /etc/apparmor.d/cache. The cache is based on mtime of the source profile and
// of the cached profile. The cache is unaware of the kernel feature set.  This
// was fixed in apparmor 2.13 where cache does not need to be purged as
// aggressively.
//
// Binary profiles have the same file name as the source profile.
// Binary profiles, just like source profiles, can contain multiple actual
// profiles. Binary profile can be directly loaded into the kernel without any
// extra processing.
//
// Apart from the aforementioned directories in /etc/apparmor.d/, at least on
// some distributions, there are more binary profiles present in
// /var/cache/apparmor and more source profiles present in
// /var/lib/snapd/apparmor/profiles. Profiles stored there don't follow the
// path convention, they can have arbitrary names.
type Legacy struct {
	sysSourceDir  string // /etc/apparmor.d/
	sysCacheDir   string // /etc/apparmor.d/cache
	extraCacheDir string // /var/cache/apparmor

	snapdSourceDir string // /var/lib/snapd/apparmor/profiles
}

// NewLegacy returns a legacy apparmor system rooted at a given directory.
func NewLegacy(rootDir string) *Legacy {
	if rootDir == "" {
		rootDir = "/"
	}
	etcAppArmorD := "etc/apparmor.d"
	return &Legacy{
		sysSourceDir:   filepath.Join(rootDir, etcAppArmorD),
		sysCacheDir:    filepath.Join(rootDir, etcAppArmorD, "cache"),
		extraCacheDir:  filepath.Join(rootDir, "var/cache/apparmor"),
		snapdSourceDir: filepath.Join(rootDir, "var/lib/snapd/apparmor/profiles"),
	}
}

// RemoveBinaryProfile removes binary profile with a given file name.
//
// The profile is removed, if it exists, from both the system and alternate
// cache directories. It is not an error if the binary profile does not exist.
func (aa *Legacy) RemoveBinaryProfile(fname string) error {
	err := os.Remove(filepath.Join(aa.sysCacheDir, fname))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	err = os.Remove(filepath.Join(aa.extraCacheDir, fname))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
