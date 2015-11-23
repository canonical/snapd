// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mvo5/goconfigparser"

	"github.com/ubuntu-core/snappy/coreconfig"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/partition"
	"github.com/ubuntu-core/snappy/pkg"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/provisioning"
)

// SystemImagePart have constant name and origin.
const (
	SystemImagePartName = "ubuntu-core"
	// SystemImagePartOrigin is the origin of any system image part
	SystemImagePartOrigin = "ubuntu"
)

const (
	// location of the channel config on the filesystem.
	//
	// This file specifies the s-i version installed on the rootfs
	// and hence s-i updates this file on every update applied to
	// the rootfs (by unpacking file "version-$version.tar.xz").
	systemImageChannelConfig = "/etc/system-image/config.d/01_channel.ini"

	// location of the client config.
	//
	// The full path to this file needs to be passed to
	// systemImageCli when querying a different rootfs.
	systemImageClientConfig = "/etc/system-image/config.d/00_default.ini"
)

var (
	// the system-image-cli binary
	systemImageCli = "system-image-cli"
)

// will replace newPartition() to return a mockPartition
var newPartition = newPartitionImpl

func newPartitionImpl() (p partition.Interface) {
	return partition.New()
}

// SystemImagePart represents a "core" snap that is managed via the SystemImage
// client
type SystemImagePart struct {
	version        string
	versionDetails string
	channelName    string
	lastUpdate     time.Time

	isInstalled bool
	isActive    bool

	updateSize int64

	partition partition.Interface
}

// Type returns pkg.TypeCore for this snap
func (s *SystemImagePart) Type() pkg.Type {
	return pkg.TypeCore
}

// Name returns the name
func (s *SystemImagePart) Name() string {
	return SystemImagePartName
}

// Origin returns the origin ("ubuntu")
func (s *SystemImagePart) Origin() string {
	return SystemImagePartOrigin
}

// Version returns the version
func (s *SystemImagePart) Version() string {
	return s.version
}

// Description returns the description
func (s *SystemImagePart) Description() string {
	return "A secure, minimal transactional OS for devices and containers."
}

// Hash returns the hash
func (s *SystemImagePart) Hash() string {
	hasher := sha512.New()
	hasher.Write([]byte(s.versionDetails))
	hexdigest := hex.EncodeToString(hasher.Sum(nil))

	return hexdigest
}

// IsActive returns true if the snap is active
func (s *SystemImagePart) IsActive() bool {
	return s.isActive
}

// IsInstalled returns true if the snap is installed
func (s *SystemImagePart) IsInstalled() bool {
	return s.isInstalled
}

// InstalledSize returns the size of the installed snap
func (s *SystemImagePart) InstalledSize() int64 {
	return -1
}

// DownloadSize returns the dowload size
func (s *SystemImagePart) DownloadSize() int64 {
	return s.updateSize
}

// Date returns the last update date
func (s *SystemImagePart) Date() time.Time {
	return s.lastUpdate
}

// SetActive sets the snap active
func (s *SystemImagePart) SetActive(active bool, pb progress.Meter) error {
	isNextBootOther := s.partition.IsNextBootOther()
	isActive := s.IsActive()

	// * active
	// | * isActive
	// | | * isNextBootOther
	// | | |
	// F F F nop
	// F F T toggle
	// F T F toggle
	// F T T nop
	// T F F toggle
	// T F T nop
	// T T F nop
	// T T T toggle
	//
	// ∴ this function is the parity (a.k.a. XOR, ⊻) of these inputs \o/
	// ( and, ∀ p, q boolean: p ⊻ q ⇔ p ≠ q )
	if active != isActive != isNextBootOther {
		return s.partition.ToggleNextBoot()
	}

	return nil
}

// override in tests
var bootloaderDir = bootloaderDirImpl

func bootloaderDirImpl() string {
	return partition.BootloaderDir()
}

// Install installs the snap
func (s *SystemImagePart) Install(pb progress.Meter, flags InstallFlags) (name string, err error) {
	if provisioning.IsSideLoaded(bootloaderDir()) {
		return "", ErrSideLoaded
	}

	if pb != nil {
		// ensure the progress finishes when we are done
		defer func() {
			pb.Finished()
		}()
	}

	// Ensure there is always a kernel + initrd to boot with, even
	// if the update does not provide new versions.
	if s.needsBootAssetSync() {
		if pb != nil {
			pb.Notify("Syncing boot files")
		}
		err = s.partition.SyncBootloaderFiles(bootAssetFilePaths())
		if err != nil {
			return "", err
		}
	}

	// find out what config file to use, the other partition may be
	// empty so we need to fallback to the current one if it is
	configFile := systemImageClientConfig
	err = s.partition.RunWithOther(partition.RO, func(otherRoot string) (err error) {
		// XXX: Note that systemImageDownloadUpdate() requires
		// the s-i _client_ config file whereas otherIsEmpty()
		// checks the s-i _channel_ config file.
		otherConfigFile := filepath.Join(dirs.GlobalRootDir, otherRoot, systemImageClientConfig)
		if !otherIsEmpty(otherRoot) && helpers.FileExists(otherConfigFile) {
			configFile = otherConfigFile
		}

		// NOTE: we need to pass the config dir here
		configDir := filepath.Dir(configFile)
		return systemImageDownloadUpdate(configDir, pb)
	})
	if err != nil {
		return "", err
	}

	// Check that the final system state is as expected.
	if err = s.verifyUpgradeWasApplied(); err != nil {
		return "", err
	}

	// XXX: ToggleNextBoot() calls handleAssets() (but not SyncBootloader
	//      files :/) - handleAssets() may copy kernel/initramfs to the
	//      sync mounted /boot/uboot, so its very slow, tell the user
	//      at least that something is going on
	if pb != nil {
		pb.Notify("Updating boot files")
	}
	if err = s.partition.ToggleNextBoot(); err != nil {
		return "", err
	}
	return SystemImagePartName, nil
}

// Ensure the expected version update was applied to the expected partition.
func (s *SystemImagePart) verifyUpgradeWasApplied() error {
	// The upgrade has now been applied, so check that the expected
	// update was applied by comparing "self" (which is the newest
	// system-image revision with that installed on the other
	// partition.

	// Determine the latest installed part.
	latestPart := makeOtherPart(s.partition)
	if latestPart == nil {
		// If there is no other part, this system must be a
		// single rootfs one, so re-query current to find the
		// latest installed part.
		latestPart = makeCurrentPart(s.partition)
	}

	if latestPart == nil {
		return &ErrUpgradeVerificationFailed{
			Msg: "could not find latest installed partition",
		}
	}

	if s.version != latestPart.Version() {
		return &ErrUpgradeVerificationFailed{
			Msg: fmt.Sprintf("found %q but expected %q", latestPart.Version(), s.version),
		}
	}

	return nil
}

// Uninstall can not be used for "core" snaps
func (s *SystemImagePart) Uninstall(progress.Meter) error {
	return ErrPackageNotRemovable
}

// Config is used to to configure the snap
func (s *SystemImagePart) Config(configuration []byte) (newConfig string, err error) {
	if cfg := string(configuration); cfg != "" {
		return coreconfig.Set(cfg)
	}

	return coreconfig.Get()
}

// NeedsReboot returns true if the snap becomes active on the next reboot
func (s *SystemImagePart) NeedsReboot() bool {

	if !s.IsActive() && s.partition.IsNextBootOther() {
		return true
	}

	return false
}

// Channel returns the system-image-server channel used
func (s *SystemImagePart) Channel() string {
	return s.channelName
}

// Icon returns the icon path
func (s *SystemImagePart) Icon() string {
	return ""
}

// Frameworks returns the list of frameworks needed by the snap
func (s *SystemImagePart) Frameworks() ([]string, error) {
	// system image parts can't depend on frameworks.
	return nil, nil
}

// SystemImageRepository is the type used for the system-image-server
type SystemImageRepository struct {
	partition partition.Interface
}

// NewSystemImageRepository returns a new SystemImageRepository
func NewSystemImageRepository() *SystemImageRepository {
	// check if we are runnign on an all-snappy system and if
	// so do not create a SystemImageRepository
	configFile := filepath.Join(dirs.GlobalRootDir, systemImageChannelConfig)
	if !helpers.FileExists(configFile) {
		return nil
	}

	return &SystemImageRepository{partition: newPartition()}
}

func makePartFromSystemImageConfigFile(p partition.Interface, channelIniPath string, isActive bool) (part Part, err error) {
	cfg := goconfigparser.New()
	f, err := os.Open(channelIniPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	err = cfg.Read(f)
	if err != nil {
		logger.Noticef("Can not parse config %q: %v", channelIniPath, err)
		return nil, err
	}
	st, err := os.Stat(channelIniPath)
	if err != nil {
		logger.Noticef("Can not stat %q: %v", channelIniPath, err)
		return nil, err
	}

	currentBuildNumber, err := cfg.Get("service", "build_number")
	versionDetails, err := cfg.Get("service", "version_detail")
	channelName, err := cfg.Get("service", "channel")
	return &SystemImagePart{
		isActive:       isActive,
		isInstalled:    true,
		version:        currentBuildNumber,
		versionDetails: versionDetails,
		channelName:    channelName,
		lastUpdate:     st.ModTime(),
		partition:      p}, err
}

// Returns the part associated with the current rootfs
func makeCurrentPart(p partition.Interface) Part {
	configFile := filepath.Join(dirs.GlobalRootDir, systemImageChannelConfig)
	part, err := makePartFromSystemImageConfigFile(p, configFile, true)
	if err != nil {
		return nil
	}
	return part
}

// otherIsEmpty returns true if the rootfs path specified should be
// considered empty.
//
// Note that the rootfs _may_ not actually be strictly empty, but it
// must be considered empty if it is incomplete.
//
// This function encapsulates the heuristics to determine if the rootfs
// is complete.
func otherIsEmpty(root string) bool {
	configFile := filepath.Join(dirs.GlobalRootDir, root, systemImageChannelConfig)

	st, err := os.Stat(configFile)

	if err == nil && st.Size() > 0 {
		// the channel config file exists and has a "reasonable"
		// size. The upgrade therefore completed successfully,
		// so consider the rootfs complete.
		return false
	}

	// The upgrader pre-creates the s-i channel config file as a
	// zero-sized file when "other" is first provisioned.
	//
	// So if this file either does not exist, or has a size of zero
	// (indicating the upgrader failed to complete the s-i unpack on
	// a previous boot [which would have made configFile >0 bytes]),
	// the other partition is considered empty.

	return true
}

// Returns the part associated with the other rootfs (if any)
func makeOtherPart(p partition.Interface) Part {
	var part Part
	err := p.RunWithOther(partition.RO, func(otherRoot string) (err error) {
		if otherIsEmpty(otherRoot) {
			return nil
		}

		configFile := filepath.Join(dirs.GlobalRootDir, otherRoot, systemImageChannelConfig)
		part, err = makePartFromSystemImageConfigFile(p, configFile, false)
		if err != nil {
			logger.Noticef("Can not make system-image part for %q: %v", configFile, err)
		}
		return err
	})
	if err == partition.ErrNoDualPartition {
		return nil
	}
	return part
}

// Description describes the repository
func (s *SystemImageRepository) Description() string {
	return "SystemImageRepository"
}

// Search searches the SystemImageRepository for the given terms
func (s *SystemImageRepository) Search(terms string) (versions []Part, err error) {
	if strings.Contains(terms, SystemImagePartName) {
		part := makeCurrentPart(s.partition)
		versions = append(versions, part)
	}
	return versions, err
}

// Details returns details for the given snap
func (s *SystemImageRepository) Details(name string, origin string) ([]Part, error) {
	if name == SystemImagePartName && origin == SystemImagePartOrigin {
		return []Part{makeCurrentPart(s.partition)}, nil
	}

	return nil, ErrPackageNotFound
}

// Updates returns the available updates
func (s *SystemImageRepository) Updates() ([]Part, error) {
	configFile := filepath.Join(dirs.GlobalRootDir, systemImageChannelConfig)
	updateStatus, err := systemImageClientCheckForUpdates(configFile)
	if err != nil {
		return nil, err
	}

	current := makeCurrentPart(s.partition)
	// no VersionCompare here because the channel provides a "order" and
	// that may go backwards when switching channels(?)
	if current.Version() != updateStatus.targetVersion {
		return []Part{&SystemImagePart{
			version:        updateStatus.targetVersion,
			versionDetails: updateStatus.targetVersionDetails,
			lastUpdate:     updateStatus.lastUpdate,
			updateSize:     updateStatus.updateSize,
			channelName:    current.(*SystemImagePart).channelName,
			partition:      s.partition}}, nil
	}

	return nil, nil
}

// Installed returns the installed snaps from this repository
func (s *SystemImageRepository) Installed() ([]Part, error) {
	var parts []Part

	// current partition
	curr := makeCurrentPart(s.partition)
	if curr != nil {
		parts = append(parts, curr)
	}

	// other partition
	other := makeOtherPart(s.partition)
	if other != nil {
		parts = append(parts, other)
	}

	return parts, nil
}

// All installed parts. SystemImageParts are non-removable.
func (s *SystemImageRepository) All() ([]Part, error) {
	return s.Installed()
}

// needsSync determines if syncing boot assets is required
func (s *SystemImagePart) needsBootAssetSync() bool {
	// current partition
	curr := makeCurrentPart(s.partition)
	if curr == nil {
		// this should never ever happen
		panic("current part does not exist")
	}

	// other partition
	other := makeOtherPart(s.partition)
	if other == nil {
		return true
	}

	// the idea here is that a channel change on the other
	// partition always triggers a full image download so
	// there is no need for syncing the assets (because the
	// kernel is included in the full image already)
	//
	// FIXME: its not entirely clear if this is true, there
	// is no mechanism to switch channels right now and all
	// the tests always switch both partitions
	if curr.Channel() != other.Channel() {
		return false
	}

	// if the other version is already a higher version number
	// than the current one it means all the kernel updates
	// has happend already and we do not need to sync the
	// bootloader files, see:
	// https://bugs.launchpad.net/snappy/+bug/1474125
	return VersionCompare(curr.Version(), other.Version()) > 0
}
