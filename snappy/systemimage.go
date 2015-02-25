//--------------------------------------------------------------------
// Copyright (c) 2014-2015 Canonical Ltd.
//--------------------------------------------------------------------

package snappy

import (
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	partition "launchpad.net/snappy/partition"

	"github.com/mvo5/goconfigparser"
)

const (
	systemImagePartName = "ubuntu-core"

	// location of the channel config on the filesystem
	systemImageChannelConfig = "/etc/system-image/channel.ini"

	// the location for the ReloadConfig
	systemImageClientConfig = "/etc/system-image/client.ini"
)

var (
	// the system-image-cli binary
	systemImageCli = "system-image-cli"
)

// override for testing
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
func (s *SystemImagePart) SetActive() (err error) {
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

// Install installs the snap
func (s *SystemImagePart) Install(pb ProgressMeter) (err error) {
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
		return err
	}

	// find out what config file to use, the other partition may be
	// empty so we need to fallback to the current one if it is
	configFile := systemImageClientConfig
	err = s.partition.RunWithOther(partition.RO, func(otherRoot string) (err error) {
		otherConfigFile := filepath.Join(systemImageRoot, otherRoot, systemImageChannelConfig)
		if _, err := os.Stat(otherConfigFile); err == nil {
			configFile = otherConfigFile
		}

		err = systemImageDownloadUpdate(configFile, pb)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	return s.partition.ToggleNextBoot()
}

// Uninstall can not be used for "core" snaps
func (s *SystemImagePart) Uninstall() (err error) {
	return errors.New("Uninstall of a core snap is not possible")
}

// Config is used to to configure the snap
func (s *SystemImagePart) Config(configuration []byte) (new string, err error) {
	// system-image is special and we provide a ubuntu-core-config
	// script via cloud-init
	const coreConfig = "/usr/bin/ubuntu-core-config"
	return runConfigScript(coreConfig, string(configuration), nil)
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

// SystemImageRepository is the type used for the system-image-server
type SystemImageRepository struct {
	partition partition.Interface
	myroot    string
}

// NewSystemImageRepository returns a new SystemImageRepository
func NewSystemImageRepository() *SystemImageRepository {
	return &SystemImageRepository{
		partition: newPartition()}
}

// Description describes the repository
func (s *SystemImageRepository) Description() string {
	return "SystemImageRepository"
}

func (s *SystemImageRepository) makePartFromSystemImageConfigFile(path string, isActive bool) (part Part, err error) {
	cfg := goconfigparser.New()
	f, err := os.Open(path)
	if err != nil {
		return part, err
	}
	defer f.Close()
	err = cfg.Read(f)
	if err != nil {
		log.Printf("Can not parse config '%s': %s", path, err)
		return part, err
	}
	st, err := os.Stat(path)
	if err != nil {
		log.Printf("Can stat '%s': %s", path, err)
		return part, err
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
		partition:      s.partition}, err
}

func (s *SystemImageRepository) currentPart() Part {
	configFile := filepath.Join(systemImageRoot, systemImageChannelConfig)
	part, err := s.makePartFromSystemImageConfigFile(configFile, true)
	if err != nil {
		return nil
	}
	return part
}

// Returns the part associated with the other rootfs (if any)
func (s *SystemImageRepository) otherPart() Part {
	var part Part
	err := s.partition.RunWithOther(partition.RO, func(otherRoot string) (err error) {
		configFile := filepath.Join(systemImageRoot, otherRoot, systemImageChannelConfig)
		_, err = os.Stat(configFile)
		if err != nil && os.IsNotExist(err) {
			// config file doesn't exist, meaning the other
			// partition is empty. However, this is not an
			// error condition (atleast for amd64 images
			// which only have 1 partition pre-installed).
			return nil
		}
		part, err = s.makePartFromSystemImageConfigFile(configFile, false)
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

// Search searches the SystemImageRepository for the given terms
func (s *SystemImageRepository) Search(terms string) (versions []Part, err error) {
	if strings.Contains(terms, systemImagePartName) {
		part := s.currentPart()
		versions = append(versions, part)
	}
	return versions, err
}

// Details returns details for the given snap
func (s *SystemImageRepository) Details(snapName string) (versions []Part, err error) {
	if snapName == systemImagePartName {
		part := s.currentPart()
		versions = append(versions, part)
	}
	return versions, err
}

// Updates returns the available updates
func (s *SystemImageRepository) Updates() (parts []Part, err error) {
	configFile := filepath.Join(systemImageRoot, systemImageChannelConfig)
	updateStatus, err := systemImageClientCheckForUpdates(configFile)

	current := s.currentPart()
	currentVersion := current.Version()
	if VersionCompare(currentVersion, updateStatus.targetVersion) < 0 {
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
	curr := s.currentPart()
	if curr != nil {
		parts = append(parts, curr)
	}

	// other partition
	other := s.otherPart()
	if other != nil {
		parts = append(parts, other)
	}

	return parts, err
}
