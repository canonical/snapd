package snappy

import (
	"fmt"
	"log"
	"reflect"
	"runtime"
	"testing"

	. "gopkg.in/check.v1"
	dbus "launchpad.net/go-dbus/v1"
)

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type DBusService struct {
	conn    *dbus.Connection
	msgChan chan *dbus.Message

	BusInterface string
	BusPath      string
	BusName      string

	actor interface{}
}

type DBusServiceActor interface {
	SetOwner(s *DBusService)
}

func NewDBusService(conn *dbus.Connection, interf, path, name string, actor DBusServiceActor) *DBusService {
	s := &DBusService{
		conn:         conn,
		msgChan:      make(chan *dbus.Message),
		BusInterface: interf,
		BusPath:      path,
		BusName:      name,
		actor:        actor}
	runtime.SetFinalizer(s, cleanupDBusService)

	nameOnBus := conn.RequestName(name, dbus.NameFlagDoNotQueue)
	err := <-nameOnBus.C
	if err != nil {
		fmt.Errorf("bus name coule not be taken %s", err)
		return nil
	}
	go s.watchBus()
	conn.RegisterObjectPath(dbus.ObjectPath(path), s.msgChan)

	actor.SetOwner(s)
	return s
}

func (s *DBusService) SendSignal(signal *dbus.Message) error {
	err := s.conn.Send(signal)
	return err
}

func (s *DBusService) watchBus() {
	for msg := range s.msgChan {
		var reply *dbus.Message
		switch {
		case msg.Interface == s.BusInterface:
			methodName := msg.Member
			m := reflect.ValueOf(s.actor).MethodByName(methodName)
			if !m.IsValid() {
				reply = dbus.NewErrorMessage(msg, "method-not-found", fmt.Sprintf("method %s not found for %s", methodName, s.actor))
				break
			}
			allArgs := msg.AllArgs()
			params := make([]reflect.Value, len(allArgs))
			for i, arg := range allArgs {
				params[i] = reflect.ValueOf(arg)
			}
			// FIMXE: check if params match the method signature
			ret := m.Call(params)
			// FIXME: check we always get at least one value
			//        back
			errVal := ret[len(ret)-1]
			if !errVal.IsNil() {
				reply = dbus.NewErrorMessage(msg, "method-returned-error", fmt.Sprintf("%v", reflect.ValueOf(errVal)))
				break
			}
			reply = dbus.NewMethodReturnMessage(msg)
			for i := 0; i < len(ret)-1; i++ {
				reply.AppendArgs(ret[i].Interface())
			}
		default:
			log.Println("unknown method call %v", msg)
		}
		if err := s.conn.Send(reply); err != nil {
			log.Println("could not send reply:", err)
		}
	}
}

func cleanupDBusService(s *DBusService) {
	s.conn.UnregisterObjectPath(dbus.ObjectPath(s.BusPath))
	close(s.msgChan)
}

type MockSystemImage struct {
	service *DBusService

	info map[string]string
}

func NewMockSystemImage() *MockSystemImage {
	msi := new(MockSystemImage)
	msi.info = make(map[string]string)

	return msi
}

func (m *MockSystemImage) SetOwner(service *DBusService) {
	m.service = service
}

func (m *MockSystemImage) Information() (map[string]string, error) {
	return m.info, nil
}

func (m *MockSystemImage) CheckForUpdate() error {
	sig := dbus.NewSignalMessage(SYSTEM_IMAGE_OBJECT_PATH, SYSTEM_IMAGE_INTERFACE, "UpdateAvailableStatus")

	var size int32 = 1234
	sig.AppendArgs(
		true,               // is_available
		false,              // downloading
		"3.14",             // available_version
		size,               // update_size
		"late_update_date", // laste update date
		"")                 // error_reason

	if err := m.service.SendSignal(sig); err != nil {
		// FIXME: do something with the error
		panic(err)
	}
	return nil
}

func (m *MockSystemImage) GetSetting(key string) (string, error) {
	return fmt.Sprintf("value-of: %s", key), nil
}


type SITestSuite struct{
	conn *dbus.Connection
	mockSystemImage *MockSystemImage
	systemImage *SystemImageRepository
	
	mockService *DBusService
}

var _ = Suite(&SITestSuite{})

func (s *SITestSuite) SetUpTest(c *C) {
	var err error
	s.conn, err = dbus.Connect(dbus.SessionBus)
	c.Assert(err, IsNil)

	s.mockSystemImage = NewMockSystemImage()
	s.mockService = NewDBusService(s.conn, SYSTEM_IMAGE_INTERFACE, SYSTEM_IMAGE_OBJECT_PATH, SYSTEM_IMAGE_BUS_NAME, s.mockSystemImage)
	c.Assert(s.mockService, NotNil)

	s.systemImage = newSystemImageRepositoryForBus(dbus.SessionBus)
	c.Assert(s, NotNil)

	// ensure we always have something installed
	s.mockSystemImage.info["current_build_number"] = "2.71"
	s.mockSystemImage.info["version_details"] = "ubuntu=20141206,raw-device=20141206,version=77"
}

func (s *SITestSuite) TearDownTest(c *C) {
	s.conn.Close()
}

func (s *SITestSuite) TestLowLevelInformation(c *C) {
	err := s.systemImage.information()
	c.Assert(err, IsNil)
	c.Assert(s.systemImage.info["current_build_number"], Equals, "2.71")
}

func (s *SITestSuite) TestLowLevelGetSetting(c *C) {
	value, err := s.systemImage.getSetting("all-cool")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "value-of: all-cool")
}

func (s *SITestSuite) TestTestInstalled(c *C) {
	// whats installed
	parts, err := s.systemImage.GetInstalled()
	c.Assert(err, IsNil)
	c.Assert(len(parts), Equals, 1)
	c.Assert(parts[0].Name(), Equals, "ubuntu-core")
	c.Assert(parts[0].Version(), Equals, "2.71")
	c.Assert(parts[0].Hash(), Equals, "bf3e9dd92c916d3fa70bbdf5a1014a112fb45b95179ecae0be2836ea2bd91f7f")
}

func (s *SITestSuite) TestGetUpdateNoUpdate(c *C) {
	parts, err := s.systemImage.GetUpdates()
	c.Assert(err, IsNil)
	c.Assert(len(parts), Equals, 0)
}

func (s *SITestSuite) TestGetUpdateHasUpdate(c *C) {
	// add a update
	s.mockSystemImage.info["target_build_number"] = "3.14"
	parts, err := s.systemImage.GetUpdates()
	c.Assert(err, IsNil)
	c.Assert(len(parts), Equals, 1)
	c.Assert(parts[0].Name(), Equals, "ubuntu-core")
	c.Assert(parts[0].Version(), Equals, "3.14")
}
