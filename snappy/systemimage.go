//--------------------------------------------------------------------
// Copyright (c) 2014-2015 Canonical Ltd.
//--------------------------------------------------------------------

package snappy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	partition "launchpad.net/snappy/partition"

	"github.com/mvo5/goconfigparser"
	dbus "launchpad.net/go-dbus/v1"
)

const (
	systemImageBusName    = "com.canonical.SystemImage"
	systemImageObjectPath = "/Service"
	systemImageInterface  = systemImageBusName

	// XXX: arbitrary value, but surely sufficient?
	systemImageTimeoutSecs = 30

	systemImagePartName = "ubuntu-core"

	// location of the channel config on the filesystem
	systemImageChannelConfig = "/etc/system-image/channel.ini"

	// the location for the ReloadConfig
	systemImageClientConfig = "/etc/system-image/client.ini"
)

// SystemImagePart represents a "core" snap that is managed via the SystemImage
// client
type SystemImagePart struct {
	proxy *systemImageDBusProxy

	version        string
	versionDetails string
	channelName    string

	isInstalled bool
	isActive    bool

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
	hasher := sha256.New()
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
func (s *SystemImagePart) InstalledSize() int {
	return -1
}

// DownloadSize returns the dowload size
func (s *SystemImagePart) DownloadSize() int {
	return -1
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

	// FIXME: UpdateBootloader is a bit generic, this should really be
	//        something like ToggleNextBootToOtherParition (but slightly
	//        shorter ;)
	return s.partition.UpdateBootloader()
}

// Install installs the snap
func (s *SystemImagePart) Install(pb ProgressMeter) (err error) {
	var updateProgress *SensibleWatch
	if pb != nil {
		updateProgress, err = s.proxy.makeWatcher("UpdateProgress")
		if err != nil {
			log.Panic(fmt.Sprintf("ERROR: %v", err))
			return nil
		}

		pb.Start(100.0)
		go func() {
			var percent int32
			var eta float64
			for msg := range updateProgress.C {
				if err := msg.Args(&percent, &eta); err != nil {
					break
				}
				if percent >= 0 {
					pb.Set(float64(percent))
				} else {
					pb.Spin("Applying")
				}
			}
		}()
	}

	// Ensure there is always a kernel + initrd to boot with, even
	// if the update does not provide new versions.
	err = s.partition.SyncBootloaderFiles()
	if err != nil {
		return err
	}

	err = s.proxy.DownloadUpdate()
	if err != nil {
		return err
	}

	// Check that the final system state is as expected.
	s.verifyUpgradeWasApplied()

	// FIXME: switch s-i daemon back to current partition
	err = s.partition.UpdateBootloader()

	if pb != nil {
		pb.Finished()
		updateProgress.Cancel()
	}
	return err
}

// Ensure the expected version update was applied to the expected partition.
func (s *SystemImagePart) verifyUpgradeWasApplied() {
	// The upgrade has now been applied, so check that the expected
	// update was applied by comparing "self" (which is the newest
	// system-image revision with that installed on the other
	// partition.

	repo := NewSystemImageRepository()

	// Determine the latest installed part.
	latestPart := repo.otherPart()
	if latestPart == nil {
		// If there is no other part, this system must be a
		// single rootfs one, so re-query current to find the
		// latest installed part.
		latestPart = repo.currentPart()
	}

	if latestPart == nil {
		log.Printf("ERROR: could not find latest installed partition")
		return
	}

	if s.version != latestPart.Version() {
		log.Printf("ERROR: found latest installed version %q (expected %q)",
		latestPart.Version(), s.version)
	}
}

// Uninstall can not be used for "core" snaps
func (s *SystemImagePart) Uninstall() (err error) {
	return errors.New("Uninstall of a core snap is not possible")
}

// Config is used to to configure the snap
func (s *SystemImagePart) Config(configuration []byte) (err error) {
	return err
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

// Result of UpdateAvailableStatus() call
type updateStatus struct {
	isAvailable      bool
	downloading      bool
	availableVersion string
	updateSize       int32
	lastUpdateDate   string
	errorReason      string
}

// Result of the Information() call
type systemImageInfo map[string]string

type systemImageDBusProxy struct {
	proxy      *dbus.ObjectProxy
	connection *dbus.Connection
	partition  partition.Interface

	// the update status
	us updateStatus

	// signal watches
	updateAvailableStatus *SensibleWatch
	updateApplied         *SensibleWatch
	updateDownloaded      *SensibleWatch
	updateFailed          *SensibleWatch
}

// this functions only exists to make testing easier, i.e. the testsuite
// will replace newPartition() to return a mockPartition
var newPartition = func() (p partition.Interface) {
	return partition.New()
}

func newSystemImageDBusProxy(bus dbus.StandardBus) *systemImageDBusProxy {
	var err error
	p := new(systemImageDBusProxy)
	p.partition = newPartition()

	if p.connection, err = dbus.Connect(bus); err != nil {
		log.Printf("Warning: can not connect to the bus")
		return nil
	}

	p.proxy = p.connection.Object(systemImageBusName, systemImageObjectPath)
	if p.proxy == nil {
		log.Printf("Warning: failed to create D-Bus proxy for system-image server")
		return nil
	}

	p.updateAvailableStatus, err = p.makeWatcher("UpdateAvailableStatus")
	if err != nil {
		log.Printf(fmt.Sprintf("Warning: %v", err))
		return nil
	}

	p.updateApplied, err = p.makeWatcher("Rebooting")
	if err != nil {
		log.Printf(fmt.Sprintf("Warning: %v", err))
		return nil
	}

	p.updateDownloaded, err = p.makeWatcher("UpdateDownloaded")
	if err != nil {
		log.Printf(fmt.Sprintf("Warning: %v", err))
		return nil
	}

	p.updateFailed, err = p.makeWatcher("UpdateFailed")
	if err != nil {
		log.Printf(fmt.Sprintf("Warning: %v", err))
		return nil
	}

	runtime.SetFinalizer(p, func(p *systemImageDBusProxy) {
		p.updateAvailableStatus.Cancel()
		p.updateApplied.Cancel()
		p.updateDownloaded.Cancel()
		p.updateFailed.Cancel()
	})

	return p
}

func (s *systemImageDBusProxy) Information() (info systemImageInfo, err error) {
	callName := "Information"
	msg, err := s.proxy.Call(systemImageBusName, callName)
	if err != nil {
		return info, err
	}

	err = msg.Args(&info)
	if err != nil {
		return info, err
	}

	// FIXME: workaround version number oddness
	if info["target_build_number"] == "-1" {
		info["target_build_number"] = "0~"
	}

	return info, nil
}

func (s *systemImageDBusProxy) GetSetting(key string) (v string, err error) {
	callName := "GetSetting"
	msg, err := s.proxy.Call(systemImageBusName, callName, key)
	if err != nil {
		return v, err
	}

	err = msg.Args(&v)
	if err != nil {
		return v, err
	}

	return v, nil
}

// SensibleWatch is a workaround for go-dbus bug #1416352 makes this
// nesessary (so sad!)
type SensibleWatch struct {
	watch  *dbus.SignalWatch
	C      chan *dbus.Message
	closed bool
}

// Cancel cancels watching
func (w *SensibleWatch) Cancel() {
	w.watch.Cancel()
}

func (s *systemImageDBusProxy) makeWatcher(signalName string) (sensibleWatch *SensibleWatch, err error) {
	watch, err := s.connection.WatchSignal(&dbus.MatchRule{
		Type:      dbus.TypeSignal,
		Sender:    systemImageBusName,
		Interface: systemImageInterface,
		Member:    signalName})
	if err != nil {
		return sensibleWatch, err
	}
	sensibleWatch = &SensibleWatch{
		watch: watch,
		C:     make(chan *dbus.Message)}
	// without this go routine we will deadlock (#1416352)
	go func() {
		for msg := range watch.C {
			sensibleWatch.C <- msg
		}
		close(sensibleWatch.C)
	}()

	return sensibleWatch, err
}

func (s *systemImageDBusProxy) ApplyUpdate() (err error) {
	callName := "ApplyUpdate"
	_, err = s.proxy.Call(systemImageBusName, callName)
	if err != nil {
		return err
	}
	select {
	case _ = <-s.updateApplied.C:
		break
	case _ = <-s.updateFailed.C:
		return errors.New("updateFailed")
	}
	return nil
}

func (s *systemImageDBusProxy) DownloadUpdate() (err error) {
	callName := "DownloadUpdate"
	_, err = s.proxy.Call(systemImageBusName, callName)
	if err != nil {
		return err
	}
	select {
	case _ = <-s.updateDownloaded.C:
		s.ApplyUpdate()
	case _ = <-s.updateFailed.C:
		return errors.New("downloadFailed")
	}

	return err
}

// Force system-image-dbus daemon to read the other partitions
// system-image configuration file so that it can calculate the correct
// upgrade path.
//
// If reset is true, force system-image to reload its configuration from
// the current rootfs, otherwise
func (s *systemImageDBusProxy) ReloadConfiguration(reset bool) (err error) {
	// Using RunWithOther() is safe since the
	// system-image-dbus daemon caches its configuration file,
	// so once the D-Bus call completes, it no longer cares
	// about configFile.
	return s.partition.RunWithOther(partition.RO, func(otherRoot string) (err error) {
		configFile := filepath.Join(otherRoot, systemImageClientConfig)
		// FIXME: replace with FileExists() call once it's in a utility
		// package.
		_, err = os.Stat(configFile)
		if err != nil && os.IsNotExist(err) {
			// file doesn't exist, making this call a NOP.
			return nil
		}
		callName := "ReloadConfiguration"
		_, err = s.proxy.Call(systemImageBusName, callName, configFile)
		return err
	})
}

// Check to see if there is a system image update available
func (s *systemImageDBusProxy) CheckForUpdate() (us updateStatus, err error) {

	// Ensure the system-image-dbus daemon is looking at the correct
	// rootfs's configuration file
	if err = s.ReloadConfiguration(false); err != nil {
		return us, err
	}
	// FIXME: we can not switch back or DownloadUpdate is unhappy

	callName := "CheckForUpdate"
	_, err = s.proxy.Call(systemImageBusName, callName)
	if err != nil {
		return us, err
	}

	select {
	case msg := <-s.updateAvailableStatus.C:
		err = msg.Args(&s.us.isAvailable,
			&s.us.downloading,
			&s.us.availableVersion,
			&s.us.updateSize,
			&s.us.lastUpdateDate,
			&s.us.errorReason)

	case <-time.After(systemImageTimeoutSecs * time.Second):
		err = fmt.Errorf("Warning: timed out after %d seconds waiting for system image server to respond",
			systemImageTimeoutSecs)
	}

	return s.us, err
}

// SystemImageRepository is the type used for the system-image-server
type SystemImageRepository struct {
	proxy     *systemImageDBusProxy
	partition partition.Interface
	myroot    string
}

// Constructor
func newSystemImageRepositoryForBus(bus dbus.StandardBus) *SystemImageRepository {
	return &SystemImageRepository{
		proxy:     newSystemImageDBusProxy(bus),
		partition: newPartition()}
}

// NewSystemImageRepository returns a new SystemImageRepository
func NewSystemImageRepository() *SystemImageRepository {
	return newSystemImageRepositoryForBus(dbus.SystemBus)
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

	currentBuildNumber, err := cfg.Get("service", "build_number")
	versionDetails, err := cfg.Get("service", "version_detail")
	channelName, err := cfg.Get("service", "channel")
	return &SystemImagePart{
		isActive:       isActive,
		isInstalled:    true,
		proxy:          s.proxy,
		version:        currentBuildNumber,
		versionDetails: versionDetails,
		channelName:    channelName,
		partition:      s.partition}, err
}

func (s *SystemImageRepository) currentPart() Part {
	configFile := filepath.Join(s.myroot, systemImageChannelConfig)
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
		configFile := filepath.Join(s.myroot, otherRoot, systemImageChannelConfig)
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
		s.proxy.Information()
		part := s.currentPart()
		versions = append(versions, part)
	}
	return versions, err
}

// Details returns details for the given snap
func (s *SystemImageRepository) Details(snapName string) (versions []Part, err error) {
	if snapName == systemImagePartName {
		s.proxy.Information()
		part := s.currentPart()
		versions = append(versions, part)
	}
	return versions, err
}

// Updates returns the available updates
func (s *SystemImageRepository) Updates() (parts []Part, err error) {
	if _, err = s.proxy.CheckForUpdate(); err != nil {
		return parts, err
	}
	current := s.currentPart()
	currentVersion := current.Version()
	targetVersion := s.proxy.us.availableVersion

	if targetVersion == "" {
		// no newer version available
		return parts, err
	}

	if VersionCompare(currentVersion, targetVersion) < 0 {
		parts = append(parts, &SystemImagePart{
			proxy:          s.proxy,
			version:        targetVersion,
			versionDetails: "?",
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
