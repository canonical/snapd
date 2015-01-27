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

type SystemImagePart struct {
	proxy *systemImageDBusProxy

	version        string
	versionDetails string
	channelName    string

	isInstalled bool
	isActive    bool

	partition partition.PartitionInterface
}

func (s *SystemImagePart) Type() string {
	return "core"
}

func (s *SystemImagePart) Name() string {
	return systemImagePartName
}

func (s *SystemImagePart) Version() string {
	return s.version
}

func (s *SystemImagePart) Description() string {
	return "ubuntu-core description"
}

func (s *SystemImagePart) Hash() string {
	hasher := sha256.New()
	hasher.Write([]byte(s.versionDetails))
	hexdigest := hex.EncodeToString(hasher.Sum(nil))

	return hexdigest
}

func (s *SystemImagePart) IsActive() bool {
	return s.isActive
}

func (s *SystemImagePart) IsInstalled() bool {
	return s.isInstalled
}

func (s *SystemImagePart) InstalledSize() int {
	return -1
}

func (s *SystemImagePart) DownloadSize() int {
	return -1
}

func (s *SystemImagePart) Install(pb ProgressMeter) (err error) {

	quitCh := make(chan int)
	if pb != nil {
		// FIXME: we need to find a way to stop this watcher
		updateProgressCh, err := s.proxy.makeWatcher("UpdateProgress")
		if err != nil {
			log.Panic(fmt.Sprintf("ERROR: %v", err))
			return nil
		}

		defer func() {
			quitCh <- 1
		}()
		pb.Start(100.0)
		go func() {
			for {
				var percent int32
				var eta float64
				select {
				case msg := <-updateProgressCh:
					err := msg.Args(&percent, &eta)
					if err == nil {
						if percent >= 0 {
							pb.Set(float64(percent))
						} else {
							pb.Spin("Applying")
						}
					}
				case <-quitCh:
					break
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

	// FIXME: switch s-i daemon back to current partition
	err = s.partition.UpdateBootloader()

	if pb != nil {
		pb.Finished()
	}
	return err
}

func (s *SystemImagePart) Uninstall() (err error) {
	return errors.New("Uninstall of a core snapp is not possible")
}

func (s *SystemImagePart) Config(configuration []byte) (err error) {
	return err
}

// Mark the *currently* booted rootfs as "good" (it booted :)
// Note: Not part of the Part interface.
func (s *SystemImagePart) MarkBootSuccessful() (err error) {

	return s.partition.MarkBootSuccessful()
}
func (s *SystemImagePart) Channel() string {

	return s.channelName
}

// Return true if the next boot will use the other root filesystem.
func (s *SystemImagePart) NextBootIsOther() bool {
	return s.partition.NextBootIsOther()
}

// Result of UpdateAvailableStatus() call
type updateStatus struct {
	is_available      bool
	downloading       bool
	available_version string
	update_size       int32
	last_update_date  string
	error_reason      string
}

// Result of the Information() call
type systemImageInfo map[string]string

type systemImageDBusProxy struct {
	proxy      *dbus.ObjectProxy
	connection *dbus.Connection
	partition  partition.PartitionInterface

	// the update status
	us updateStatus

	// signal watches
	updateAvailableStatus chan *dbus.Message
	updateApplied         chan *dbus.Message
	updateDownloaded      chan *dbus.Message
	updateFailed          chan *dbus.Message
	quitCh                chan int // can be used to stop the watchers
}

// this functions only exists to make testing easier, i.e. the testsuite
// will replace newPartition() to return a mockPartition
var newPartition = func() (p partition.PartitionInterface) {
	return partition.New()
}

func newSystemImageDBusProxy(bus dbus.StandardBus) *systemImageDBusProxy {
	var err error
	p := new(systemImageDBusProxy)
	p.partition = newPartition()

	if p.connection, err = dbus.Connect(bus); err != nil {
		log.Panic("Error: can not connect to the bus")
		return nil
	}

	p.proxy = p.connection.Object(systemImageBusName, systemImageObjectPath)
	if p.proxy == nil {
		log.Panic("ERROR: failed to create D-Bus proxy for system-image server")
		return nil
	}

	p.quitCh = make(chan int)
	p.updateAvailableStatus, err = p.makeWatcher("UpdateAvailableStatus")
	if err != nil {
		log.Panic(fmt.Sprintf("ERROR: %v", err))
		return nil
	}

	p.updateApplied, err = p.makeWatcher("Rebooting")
	if err != nil {
		log.Panic(fmt.Sprintf("ERROR: %v", err))
		return nil
	}

	p.updateDownloaded, err = p.makeWatcher("UpdateDownloaded")
	if err != nil {
		log.Panic(fmt.Sprintf("ERROR: %v", err))
		return nil
	}

	p.updateFailed, err = p.makeWatcher("UpdateFailed")
	if err != nil {
		log.Panic(fmt.Sprintf("ERROR: %v", err))
		return nil
	}

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

func (s *systemImageDBusProxy) makeWatcher(signalName string) (received chan *dbus.Message, err error) {
	watch, err := s.connection.WatchSignal(&dbus.MatchRule{
		Type:      dbus.TypeSignal,
		Sender:    systemImageBusName,
		Interface: systemImageInterface,
		Member:    signalName})
	if err != nil {
		return received, err
	}
	runtime.SetFinalizer(watch, func(u *dbus.SignalWatch) {
		u.Cancel()
	})

	received = make(chan *dbus.Message)
	go func() {
		for {
			select {
			case msg := <-watch.C:
				received <- msg
			}
		}
	}()

	return received, err
}

func (s *systemImageDBusProxy) ApplyUpdate() (err error) {
	callName := "ApplyUpdate"
	_, err = s.proxy.Call(systemImageBusName, callName)
	if err != nil {
		return err
	}
	select {
	case _ = <-s.updateApplied:
		break
	case _ = <-s.updateFailed:
		return errors.New("updateFailed")
		break
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
	case _ = <-s.updateDownloaded:
		s.ApplyUpdate()
	case _ = <-s.updateFailed:
		return errors.New("downloadFailed")
		break
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
	return s.partition.RunWithOther(func(otherRoot string) (err error) {
		configFile := otherRoot + systemImageClientConfig
		// FIXME: replace with FileExists() call once it's in a utility
		// package.
		_, err = os.Stat(configFile)
		if err != nil {
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

	callName := "CheckForUpdate"
	_, err = s.proxy.Call(systemImageBusName, callName)
	if err != nil {
		return us, err
	}

	select {
	case msg := <-s.updateAvailableStatus:
		err = msg.Args(&s.us.is_available,
			&s.us.downloading,
			&s.us.available_version,
			&s.us.update_size,
			&s.us.last_update_date,
			&s.us.error_reason)

	case <-time.After(systemImageTimeoutSecs * time.Second):
		err = errors.New(fmt.Sprintf(
			"ERROR: "+
				"timed out after %d seconds "+
				"waiting for system image server to respond",
			systemImageTimeoutSecs))
	}

	// switch back to using the current rootfs's system-image
	// configuration.
	if err = s.ReloadConfiguration(true); err != nil {
		return us, err
	}

	return s.us, err
}

type SystemImageRepository struct {
	proxy     *systemImageDBusProxy
	partition partition.PartitionInterface
	myroot    string
}

// Constructor
func newSystemImageRepositoryForBus(bus dbus.StandardBus) *SystemImageRepository {
	return &SystemImageRepository{
		proxy:     newSystemImageDBusProxy(bus),
		partition: newPartition()}
}

func NewSystemImageRepository() *SystemImageRepository {
	return newSystemImageRepositoryForBus(dbus.SystemBus)
}

func (s *SystemImageRepository) Description() string {
	return "SystemImageRepository"
}

func (s *SystemImageRepository) makePartFromSystemImageConfigFile(path string, isActive bool) (part Part, err error) {
	cfg := goconfigparser.New()
	f, err := os.Open(path)
	if err != nil {
		log.Printf("Can not open '%s': %s", path, err)
		return part, err
	}
	err = cfg.Read(f)
	if err != nil {
		log.Printf("Can not parse config '%s': err", path, err)
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

func (s *SystemImageRepository) getCurrentPart() Part {
	configFile := s.myroot + systemImageChannelConfig
	part, err := s.makePartFromSystemImageConfigFile(configFile, true)
	if err != nil {
		log.Printf("Can not make system-image part for %s: %s", configFile, err)
	}
	return part
}

// Returns the part associated with the other rootfs (if any)
func (s *SystemImageRepository) getOtherPart() Part {
	var part Part
	s.partition.RunWithOther(func(otherRoot string) (err error) {
		configFile := s.myroot + otherRoot + systemImageChannelConfig
		part, err = s.makePartFromSystemImageConfigFile(configFile, false)
		if err != nil {
			log.Printf("Can not make system-image part for %s: %s", configFile, err)
		}
		return err
	})
	return part
}

func (s *SystemImageRepository) Search(terms string) (versions []Part, err error) {
	if strings.Contains(terms, systemImagePartName) {
		s.proxy.Information()
		part := s.getCurrentPart()
		versions = append(versions, part)
	}
	return versions, err
}

func (s *SystemImageRepository) Details(snappName string) (versions []Part, err error) {
	if snappName == systemImagePartName {
		s.proxy.Information()
		part := s.getCurrentPart()
		versions = append(versions, part)
	}
	return versions, err
}

func (s *SystemImageRepository) GetUpdates() (parts []Part, err error) {

	if _, err = s.proxy.CheckForUpdate(); err != nil {
		return parts, err
	}
	info, err := s.proxy.Information()
	if err != nil {
		return parts, err
	}
	if VersionCompare(info["current_build_number"], info["target_build_number"]) < 0 {
		version := info["target_build_number"]
		parts = append(parts, &SystemImagePart{
			proxy:          s.proxy,
			version:        version,
			versionDetails: info["version_details"],
			channelName:    info["channel_name"],
			partition:      s.partition})
	}

	return parts, err
}

func (s *SystemImageRepository) GetInstalled() (parts []Part, err error) {
	// current partition
	curr := s.getCurrentPart()
	if curr != nil {
		parts = append(parts, curr)
	}

	// other partition
	other := s.getOtherPart()
	if other != nil {
		parts = append(parts, other)
	}

	return parts, err
}
