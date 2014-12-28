package snappy

import (
	"fmt"
	"errors"
	"time"

	partition "launchpad.net/snappy-ubuntu/snappy-golang/partition"

	dbus "launchpad.net/go-dbus/v1"
)

const (
	SYSTEM_IMAGE_BUS_NAME     = "com.canonical.SystemImage"
	SYSTEM_IMAGE_OBJECT_PATH  = "/Service"
	SYSTEM_IMAGE_INTERFACE    = SYSTEM_IMAGE_BUS_NAME

	// XXX: arbitrary value, but surely sufficient?
	SYSTEM_IMAGE_TIMEOUT_SECS = 30
)

// Result of UpdateAvailableStatus() call
type updateStatus struct {
	is_available       bool
	downloading        bool
	available_version  string
	update_size        int32
	last_update_date   string
	error_reason       string
}

// Result of Information() method call
type SystemImage struct {
	DataSource
	proxy         *dbus.ObjectProxy
	connection    *dbus.Connection
	info           map[string]string
	UpdateStatus   updateStatus
}

// Check to see if there is a system image update available
func (s *SystemImage) checkForUpdate() (err error) {
	var updatesAvailableStatusWatch  *dbus.SignalWatch

	updatesAvailableStatusWatch, err = s.connection.WatchSignal(&dbus.MatchRule{
		Type      : dbus.TypeSignal,
		Sender    : SYSTEM_IMAGE_BUS_NAME,
		Interface : SYSTEM_IMAGE_INTERFACE,
		Member    : "UpdateAvailableStatus"})
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
			"ERROR: " +
			"timed out after %d seconds " +
			"waiting for system image server to respond",
			SYSTEM_IMAGE_TIMEOUT_SECS))
	}

	var msg *dbus.Message

	callName = "Information"
	msg, err = s.proxy.Call(SYSTEM_IMAGE_BUS_NAME, callName)
	if err != nil {
		return err
	}

	err = msg.Args(&s.info)
	if err != nil {
		return err
	}

	return nil
}

// Hook up the connection to the system-image server
func (s *SystemImage) dbusSetup() (err error) {

	if s.connection, err = dbus.Connect(dbus.SystemBus); err != nil {
		return err
	}

	s.proxy = s.connection.Object(SYSTEM_IMAGE_BUS_NAME,
				      SYSTEM_IMAGE_OBJECT_PATH)
	if s.proxy == nil {
		return errors.New("ERROR: " +
		                  "failed to create D-Bus proxy " +
				  "for system-image server")
	}

	if err = s.checkForUpdate(); err != nil {
		return err
	}

	return err
}

// Constructor
func NewSystemImage() *SystemImage {
	s := new(SystemImage)

	s.info = make(map[string]string)

	if err := s.dbusSetup(); err != nil {
		panic(fmt.Sprintf("ERROR: %v", err))
	}

	return s
}

func (s *SystemImage) Versions() (versions []Part) {
	// FIXME
	return versions
}

func (s *SystemImage) Update(parts []Part) (err error) {
	parts = s.Versions()

	p := partition.New()

	// FIXME
	fmt.Println("FIXME: blindly toggling rootfs for testing!")

	return p.UpdateBootloader()
}

func (s *SystemImage) Rollback(parts []Part) (err error) {
	// FIXME
	return err
}

func (s *SystemImage) Tags(part Part) (tags []string) {
	return tags
}

func (s *SystemImage) Less(a, b Part) bool {
	// FIXME
	return false
}

func (s *SystemImage) Privileged() bool {
	// Root required to mount filesystems, unpack images, et cetera.
	return true
}
