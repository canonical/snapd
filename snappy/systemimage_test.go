package snappy

import (
	"fmt"
	"log"
	"reflect"
	"runtime"

	dbus "launchpad.net/go-dbus/v1"
	. "launchpad.net/gocheck"
)

type DBusService struct {
	conn    *dbus.Connection
	msgChan chan *dbus.Message

	BusInterface string
	BusPath      string
	BusName      string

	actor interface{}
}

func NewDBusService(conn *dbus.Connection, interf, path, name string, actor interface{}) *DBusService {
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
	return s
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
	info map[string]string
}

func (m *MockSystemImage) information() (map[string]string, error) {
	return m.info, nil
}

func (m *MockSystemImage) getSetting(key string) (string, error) {
	return fmt.Sprintf("value-of: %s", key), nil
}

type TestSuite struct{}

func (sx *TestSuite) TestInfo(c *C) {
	conn, err := dbus.Connect(dbus.SessionBus)
	c.Assert(err, IsNil)
	defer conn.Close()

	// setUp
	mockSystemImage := new(MockSystemImage)
	mockService := NewDBusService(conn, SYSTEM_IMAGE_INTERFACE, SYSTEM_IMAGE_OBJECT_PATH, SYSTEM_IMAGE_BUS_NAME, mockSystemImage)
	c.Assert(mockService, NotNil)
	mockSystemImage.info["current_build_number"] = "3.14"
	mockSystemImage.info["version_details"] = "ubuntu=20141206,raw-device=20141206,version=77"

	s := newSystemImageRepositoryForBus(dbus.SessionBus)
	c.Assert(s, NotNil)

	// low level dbus
	err = s.information()
	c.Assert(err, IsNil)
	c.Assert(s.info["current_build_number"], Equals, "2.71")

	value, err := s.getSetting("all-cool")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "value-of: all-cool")

	// high level Repository interface

	// whats installed
	parts, err := s.GetInstalled()
	c.Assert(err, IsNil)
	c.Assert(len(parts), Equals, 1)
	c.Assert(parts[0].Name(), Equals, "ubuntu-core")
	c.Assert(parts[0].Version(), Equals, "2.71")
	c.Assert(parts[0].Hash(), Equals, "bf3e9dd92c916d3fa70bbdf5a1014a112fb45b95179ecae0be2836ea2bd91f7f")

	// no update
	parts, err = s.GetUpdates()
	c.Assert(err, IsNil)
	c.Assert(len(parts), Equals, 0)

	// add a update
	mockSystemImage.info["target_build_number"] = "3.14"
	parts, err = s.GetUpdates()
	c.Assert(err, IsNil)
	c.Assert(len(parts), Equals, 1)
	c.Assert(parts[0].Name(), Equals, "ubuntu-core")
	c.Assert(parts[0].Version(), Equals, "3.14")
}
