package snappy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	info map[string]string

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
	return err
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

// Result of Information() method call
type SystemImageRepository struct {
	proxy        *dbus.ObjectProxy
	connection   *dbus.Connection
	info         map[string]string
	UpdateStatus updateStatus
}

func (s *SystemImageRepository) Description() string {
	return "SystemImageRepository"
}

func (s *SystemImageRepository) getCurrentPart() Part {
	s.information()
	version := s.info["current_build_number"]
	part := &SystemImagePart{info: s.info,
		isActive:    true,
		isInstalled: true,
		version:     version}
	return part
}

func (s *SystemImageRepository) Search(terms string) (versions []Part, err error) {
	if strings.Contains(terms, SYSTEM_IMAGE_PART_NAME) {
		s.information()
		part := s.getCurrentPart()
		versions = append(versions, part)
	}
	return versions, err
}

func (s *SystemImageRepository) GetUpdates() (parts []Part, err error) {

	if err = s.checkForUpdate(); err != nil {
		return parts, err
	}
	// FIXME: use version number compare
	if s.info["current_build_number"] < s.info["target_build_number"] {
		version := s.info["target_build_number"]
		parts = append(parts, &SystemImagePart{
			info:    s.info,
			version: version})
	}

	return parts, err
}

func (s *SystemImageRepository) GetInstalled() (parts []Part, err error) {
	s.information()

	// current partition
	parts = append(parts, s.getCurrentPart())

	// FIXME: get data from B partition and fill it in here

	return parts, err
}

func (s *SystemImageRepository) information() (err error) {
	callName := "Information"
	msg, err := s.proxy.Call(SYSTEM_IMAGE_BUS_NAME, callName)
	if err != nil {
		return err
	}

	err = msg.Args(&s.info)
	if err != nil {
		return err
	}

	return nil
}

func (s *SystemImageRepository) getSetting(key string) (v string, err error) {
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

// Check to see if there is a system image update available
func (s *SystemImageRepository) checkForUpdate() (err error) {
	var updatesAvailableStatusWatch *dbus.SignalWatch

	updatesAvailableStatusWatch, err = s.connection.WatchSignal(&dbus.MatchRule{
		Type:      dbus.TypeSignal,
		Sender:    SYSTEM_IMAGE_BUS_NAME,
		Interface: SYSTEM_IMAGE_INTERFACE,
		Member:    "UpdateAvailableStatus"})
	if err != nil {
		return err
	}
	defer updatesAvailableStatusWatch.Cancel()

	callName := "CheckForUpdate"
	_, err = s.proxy.Call(SYSTEM_IMAGE_BUS_NAME, callName)
	if err != nil {
		return err
	}

	select {
	// block, waiting for s-i to respond to the CheckForUpdate()
	// method call
	case msg := <-updatesAvailableStatusWatch.C:
		err = msg.Args(&s.UpdateStatus.is_available,
			&s.UpdateStatus.downloading,
			&s.UpdateStatus.available_version,
			&s.UpdateStatus.update_size,
			&s.UpdateStatus.last_update_date,
			&s.UpdateStatus.error_reason)

		if err != nil {
			return err
		}

	case <-time.After(SYSTEM_IMAGE_TIMEOUT_SECS * time.Second):

		return errors.New(fmt.Sprintf(
			"ERROR: "+
				"timed out after %d seconds "+
				"waiting for system image server to respond",
			SYSTEM_IMAGE_TIMEOUT_SECS))
	}

	err = s.information()
	if err != nil {
		return err
	}

	return nil
}

// Hook up the connection to the system-image server
func (s *SystemImageRepository) dbusSetup(bus dbus.StandardBus) (err error) {

	if s.connection, err = dbus.Connect(bus); err != nil {
		return err
	}

	s.proxy = s.connection.Object(SYSTEM_IMAGE_BUS_NAME,
		SYSTEM_IMAGE_OBJECT_PATH)
	if s.proxy == nil {
		return errors.New("ERROR: " +
			"failed to create D-Bus proxy " +
			"for system-image server")
	}

	return err
}

// Constructor
func newSystemImageRepositoryForBus(bus dbus.StandardBus) *SystemImageRepository {
	s := new(SystemImageRepository)

	s.info = make(map[string]string)

	if err := s.dbusSetup(bus); err != nil {
		panic(fmt.Sprintf("ERROR: %v", err))
	}
	return s
}
func NewSystemImageRepository() *SystemImageRepository {
	return newSystemImageRepositoryForBus(dbus.SystemBus)
}

func (s *SystemImageRepository) Versions() (versions []Part) {
	// FIXME
	return versions
}

func (s *SystemImageRepository) Update(parts []Part) (err error) {
	parts = s.Versions()

	p := partition.New()

	// FIXME
	fmt.Println("FIXME: blindly toggling rootfs for testing!")

	return p.UpdateBootloader()
}
