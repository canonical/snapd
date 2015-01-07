package snappy

import (
	"fmt"
	"runtime"
	"testing"
	"log"
	"reflect"

	dbus "launchpad.net/go-dbus/v1"
)

type DBusService struct {
	conn *dbus.Connection
	msgChan chan *dbus.Message

	BusInterface string
	BusPath string
	BusName string

	actor interface{}
}

func NewDBusService(conn *dbus.Connection, interf, path, name string, actor interface{}) *DBusService {
	s := &DBusService{
		conn: conn,
		msgChan: make(chan *dbus.Message),
		BusInterface: interf,
		BusPath: path,
		BusName: name,
		actor: actor}
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
				log.Println(fmt.Sprintf("no method found %v %v ", methodName, s.actor))
				// FIXME: send dbus error back
			}
			allArgs := msg.AllArgs()
			params := make([]reflect.Value, len(allArgs))
			for i, arg := range(allArgs) {
				params[i] = reflect.ValueOf(arg)
			}
			ret := m.Call(params)
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
}

func (m *MockSystemImage) Information() (map[string]string, error) {
	log.Println("Information()")

	info := make(map[string]string)
	info["current_build_number"] = "314"
	return info, nil
}

func (m *MockSystemImage) GetSetting(key string) (string, error) {
	return fmt.Sprintf("value-of: %s", key), nil
}

func TestInfo(t *testing.T) {
	conn, err := dbus.Connect(dbus.SessionBus)
	if err != nil {
		t.Error("Failed to get session bus")
	}
	defer conn.Close()

	mockSystemImage := new(MockSystemImage)
	mockService := NewDBusService(conn, SYSTEM_IMAGE_INTERFACE, SYSTEM_IMAGE_OBJECT_PATH, SYSTEM_IMAGE_BUS_NAME, mockSystemImage)
	if mockService == nil {
		t.Error("Can not create DBusService")
	}

	s := newSystemImageRepositoryForBus(dbus.SessionBus)
	if s == nil {
		t.Error("Can not create SystemImageRepository")
	}
	err = s.Information()
	if err != nil {
		t.Error(fmt.Sprintf("call to Information created a error: %s", err))
	}
	if s.info["current_build_number"] != "314" {
		t.Error("Mock call did not work")
	}

	value, err := s.GetSetting("all-cool")
	if err != nil {
		t.Error("GetSettings returned a error")
	}
	if value != "value-of: all-cool" {
		t.Error("Mock call with arguments did not work")
	}
}
