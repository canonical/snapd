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
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"launchpad.net/snappy/coreconfig"
	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/partition"
	"launchpad.net/snappy/progress"

	"github.com/mvo5/goconfigparser"
)

const (
	systemImagePartName = "ubuntu-core"

	// location of the channel config on the filesystem.
	//
	// This file specifies the s-i version installed on the rootfs
	// and hence s-i updates this file on every update applied to
	// the rootfs (by unpacking file "version-$version.tar.xz").
	systemImageChannelConfig = "/etc/system-image/channel.ini"

	// location of the client config.
	//
	// The full path to this file needs to be passed to
	// systemImageCli when querying a different rootfs.
	systemImageClientConfig = "/etc/system-image/client.ini"

	// Full path to file, which if present, marks the system as
	// having been "sideloaded", in other words having been created
	// like this:
	//
	// "ubuntu-device-flash --device-part=unofficial-assets.tar.xz ..."
	//
	// Sideloaded systems cannot be safely upgraded since there are
	// no device-part updates on the system-image server.
	sideLoadedMarkerFile = "/var/lib/snappy/sideloaded"
)

var (
	// the system-image-cli binary
	systemImageCli = "system-image-cli"
)

// This is the root directory of the filesystem. Its only useful to
// change when writing tests
var systemImageRoot = "/"

// will replace newPartition() to return a mockPartition
var newPartition = func() (p partition.Interface) {
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

// Type returns SnapTypeCore for this snap
func (s *SystemImagePart) Type() SnapType {
	return SnapTypeCore
}

// Name returns the name
func (s *SystemImagePart) Name() string {
	return systemImagePartName
}

// Version returns the version
func (s *SystemImagePart) Version() string {
	return s.version
}

// Description returns the description
func (s *SystemImagePart) Description() string {
	return "ubuntu-core description"
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
func (s *SystemImagePart) SetActive(pb progress.Meter) (err error) {
	isNextBootOther := s.partition.IsNextBootOther()
	// active and no switch scheduled -> nothing to do
	if s.IsActive() && !isNextBootOther {
		return nil
	}
	// not currently active but switch scheduled already -> nothing to do
	if !s.IsActive() && isNextBootOther {
		return nil
	}

	return s.partition.ToggleNextBoot()
}

// sideLoadedSystem determines if the system was installed using a
// custom enablement part.
func sideLoadedSystem() bool {
	path := filepath.Join(systemImageRoot, sideLoadedMarkerFile)
	return helpers.FileExists(path)
}

// Install installs the snap
func (s *SystemImagePart) Install(pb progress.Meter, flags InstallFlags) (name string, err error) {
	if sideLoadedSystem() {
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
	err = s.partition.SyncBootloaderFiles()
	if err != nil {
		return "", err
	}

	// find out what config file to use, the other partition may be
	// empty so we need to fallback to the current one if it is
	configFile := systemImageClientConfig
	err = s.partition.RunWithOther(partition.RO, func(otherRoot string) (err error) {
		// XXX: Note that systemImageDownloadUpdate() requires
		// the s-i _client_ config file whereas otherIsEmpty()
		// checks the s-i _channel_ config file.
		otherConfigFile := filepath.Join(systemImageRoot, otherRoot, systemImageClientConfig)
		if !otherIsEmpty(otherRoot) && helpers.FileExists(otherConfigFile) {
			configFile = otherConfigFile
		}

		return systemImageDownloadUpdate(configFile, pb)
	})
	if err != nil {
		return "", err
	}

	// Check that the final system state is as expected.
	if err = s.verifyUpgradeWasApplied(); err != nil {
		return "", err
	}

	if err = s.partition.ToggleNextBoot(); err != nil {
		return "", err
	}
	return systemImagePartName, nil
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
			msg: "could not find latest installed partition",
		}
	}

	if s.version != latestPart.Version() {
		return &ErrUpgradeVerificationFailed{
			msg: fmt.Sprintf("found %q but expected %q", latestPart.Version(), s.version),
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

// MarkBootSuccessful marks the *currently* booted rootfs as "good"
// (it booted :)
// Note: Not part of the Part interface.
func (s *SystemImagePart) MarkBootSuccessful() (err error) {

	return s.partition.MarkBootSuccessful()
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
		log.Printf("Can not parse config '%s': %s", channelIniPath, err)
		return nil, err
	}
	st, err := os.Stat(channelIniPath)
	if err != nil {
		log.Printf("Can stat '%s': %s", channelIniPath, err)
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
	configFile := filepath.Join(systemImageRoot, systemImageChannelConfig)
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
	configFile := filepath.Join(systemImageRoot, root, systemImageChannelConfig)

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

		configFile := filepath.Join(systemImageRoot, otherRoot, systemImageChannelConfig)
		part, err = makePartFromSystemImageConfigFile(p, configFile, false)
		if err != nil {
			log.Printf("Can not make system-image part for %s: %s", configFile, err)
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
	if strings.Contains(terms, systemImagePartName) {
		part := makeCurrentPart(s.partition)
		versions = append(versions, part)
	}
	return versions, err
}

// Details returns details for the given snap
func (s *SystemImageRepository) Details(snapName string) (versions []Part, err error) {
	if snapName == systemImagePartName {
		part := makeCurrentPart(s.partition)
		versions = append(versions, part)
	}
	return versions, err
}

// Updates returns the available updates
func (s *SystemImageRepository) Updates() (parts []Part, err error) {
	configFile := filepath.Join(systemImageRoot, systemImageChannelConfig)
	updateStatus, err := systemImageClientCheckForUpdates(configFile)

	current := makeCurrentPart(s.partition)
	// no VersionCompare here because the channel provides a "order" and
	// that may go backwards when switching channels(?)
	if current.Version() != updateStatus.targetVersion {
		parts = append(parts, &SystemImagePart{
			version:        updateStatus.targetVersion,
			versionDetails: updateStatus.targetVersionDetails,
			lastUpdate:     updateStatus.lastUpdate,
			updateSize:     updateStatus.updateSize,
			channelName:    current.(*SystemImagePart).channelName,
			partition:      s.partition})
	}

	return parts, err
}

// Installed returns the installed snaps from this repository
func (s *SystemImageRepository) Installed() (parts []Part, err error) {
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

	return parts, err
}
