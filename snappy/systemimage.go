package snappy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"runtime"
	"strings"
	"time"

	partition "launchpad.net/snappy-ubuntu/snappy-golang/partition"

	dbus "launchpad.net/go-dbus/v1"
)

const (
	SYSTEM_IMAGE_BUS_NAME    = "com.canonical.SystemImage"
	SYSTEM_IMAGE_OBJECT_PATH = "/Service"
	SYSTEM_IMAGE_INTERFACE   = SYSTEM_IMAGE_BUS_NAME

	// XXX: arbitrary value, but surely sufficient?
	SYSTEM_IMAGE_TIMEOUT_SECS = 30
)

type SystemImagePart struct {
	info  map[string]string
	proxy *systemImageDBusProxy

	version     string
	isInstalled bool
	isActive    bool
}

const (
	SYSTEM_IMAGE_PART_NAME = "ubuntu-core"
)

func (s *SystemImagePart) Name() string {
	return SYSTEM_IMAGE_PART_NAME
}

func (s *SystemImagePart) Version() string {
	return s.version
}

func (s *SystemImagePart) Description() string {
	return "ubuntu-core description"
}

func (s *SystemImagePart) Hash() string {
	hasher := sha256.New()
	hasher.Write([]byte(s.info["version_details"]))
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

func (s *SystemImagePart) Install() (err error) {
	err = s.proxy.DownloadUpdate()
	if err != nil {
		return err
	}

	p := partition.New()
	return p.UpdateBootloader()
}

func (s *SystemImagePart) Uninstall() (err error) {
	return err
}

func (s *SystemImagePart) Config(configuration []byte) (err error) {
	return err
}

// Mark the *currently* booted rootfs as "good" (it booted :)
// Note: Not part of the Part interface.
func (s *SystemImagePart) MarkBootSuccessful() (err error) {

	p := partition.New()

	return p.MarkBootSuccessful()
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

	// the update status
	us updateStatus

	// signal watches
	updateAvailableStatus chan *dbus.Message
	updateApplied         chan *dbus.Message
	updateDownloaded      chan *dbus.Message
	updateFailed          chan *dbus.Message
}

func newSystemImageDBusProxy(bus dbus.StandardBus) *systemImageDBusProxy {
	var err error
	p := new(systemImageDBusProxy)

	if p.connection, err = dbus.Connect(bus); err != nil {
		log.Panic("Error: can not connect to the bus")
		return nil
	}

	p.proxy = p.connection.Object(SYSTEM_IMAGE_BUS_NAME, SYSTEM_IMAGE_OBJECT_PATH)
	if p.proxy == nil {
		log.Panic("ERROR: failed to create D-Bus proxy for system-image server")
		return nil
	}

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
	msg, err := s.proxy.Call(SYSTEM_IMAGE_BUS_NAME, callName)
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
	msg, err := s.proxy.Call(SYSTEM_IMAGE_BUS_NAME, callName, key)
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
		Sender:    SYSTEM_IMAGE_BUS_NAME,
		Interface: SYSTEM_IMAGE_INTERFACE,
		Member:    signalName})
	if err != nil {
		return received, err
	}
	runtime.SetFinalizer(watch, func(u *dbus.SignalWatch) {
		u.Cancel()
	})

	received = make(chan *dbus.Message)
	go func() {
		for msg := range watch.C {
			received <- msg
		}
	}()

	return received, err
}

func (s *systemImageDBusProxy) ApplyUpdate() (err error) {
	callName := "ApplyUpdate"
	_, err = s.proxy.Call(SYSTEM_IMAGE_BUS_NAME, callName)
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
	_, err = s.proxy.Call(SYSTEM_IMAGE_BUS_NAME, callName)
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

// Check to see if there is a system image update available
func (s *systemImageDBusProxy) CheckForUpdate() (us updateStatus, err error) {

	callName := "CheckForUpdate"
	_, err = s.proxy.Call(SYSTEM_IMAGE_BUS_NAME, callName)
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

	case <-time.After(SYSTEM_IMAGE_TIMEOUT_SECS * time.Second):
		err = errors.New(fmt.Sprintf(
			"ERROR: "+
				"timed out after %d seconds "+
				"waiting for system image server to respond",
			SYSTEM_IMAGE_TIMEOUT_SECS))
	}

	return s.us, err
}

type SystemImageRepository struct {
	proxy *systemImageDBusProxy
}

// Constructor
func newSystemImageRepositoryForBus(bus dbus.StandardBus) *SystemImageRepository {
	s := new(SystemImageRepository)
	s.proxy = newSystemImageDBusProxy(bus)

	return s
}
func NewSystemImageRepository() *SystemImageRepository {
	return newSystemImageRepositoryForBus(dbus.SystemBus)
}

func (s *SystemImageRepository) Description() string {
	return "SystemImageRepository"
}

func (s *SystemImageRepository) getCurrentPart() Part {
	info, err := s.proxy.Information()
	if err != nil {
		panic("proxy.Information failed")
	}
	version := info["current_build_number"]
	part := &SystemImagePart{info: info,
		isActive:    true,
		isInstalled: true,
		proxy:       s.proxy,
		version:     version}
	return part
}

func (s *SystemImageRepository) Search(terms string) (versions []Part, err error) {
	if strings.Contains(terms, SYSTEM_IMAGE_PART_NAME) {
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
			info:    info,
			proxy:   s.proxy,
			version: version})
	}

	return parts, err
}

func (s *SystemImageRepository) GetInstalled() (parts []Part, err error) {
	// current partition
	parts = append(parts, s.getCurrentPart())

	// FIXME: get data from B partition and fill it in here

	return parts, err
}
