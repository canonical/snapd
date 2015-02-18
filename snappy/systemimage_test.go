package snappy

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	partition "launchpad.net/snappy/partition"

	dbus "launchpad.net/go-dbus/v1"
	. "launchpad.net/gocheck"
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

func (s *DBusService) send(msg *dbus.Message) (err error) {
	return s.conn.Send(msg)
}

func (s *DBusService) SendSignal(signal *dbus.Message) error {
	return s.send(signal)
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
			log.Printf("unknown method call %v\n", msg)
		}
		if err := s.send(reply); err != nil {
			log.Println("could not send reply:", err)
		}
	}
}

func cleanupDBusService(s *DBusService) {
	s.conn.UnregisterObjectPath(dbus.ObjectPath(s.BusPath))
	close(s.msgChan)
}

type MockSystemImage struct {
	service   *DBusService
	partition *partition.Partition

	fakeAvailableVersion string
	info                 map[string]string
}

func newMockSystemImage() *MockSystemImage {
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

func (m *MockSystemImage) ReloadConfiguration(string) error {
	return nil
}

func (m *MockSystemImage) CheckForUpdate() error {
	sig := dbus.NewSignalMessage(systemImageObjectPath, systemImageInterface, "UpdateAvailableStatus")

	// FIXME: the data we send in the signal is currently mostly
	//        irrelevant as SystemImageRepository will recv the
	//        signal but won't use the data and calls Information()
	//        again instead
	var size int32 = 1234
	sig.AppendArgs(
		true,  // is_available
		false, // downloading
		m.fakeAvailableVersion, // available_version
		size, // update_size
		"2022-03-04 05:06:07", // last update date
		"") // error_reason

	if err := m.service.SendSignal(sig); err != nil {
		// FIXME: do something with the error
		panic(err)
	}

	return nil
}

func (m *MockSystemImage) DownloadUpdate() error {
	// send progress
	for i := 1; i <= 5; i++ {
		sig := dbus.NewSignalMessage(systemImageObjectPath, systemImageInterface, "UpdateProgress")
		sig.AppendArgs(
			int32(20*i),             // percent (int32)
			float64(100.0-(20.0*i)), // eta (double)
		)
		if err := m.service.SendSignal(sig); err != nil {
			// FIXME: do something with the error
			panic(err)
		}
	}

	// send done
	sig := dbus.NewSignalMessage(systemImageObjectPath, systemImageInterface, "UpdateDownloaded")

	sig.AppendArgs(
		true, // status, true if a reboot is required
	)
	if err := m.service.SendSignal(sig); err != nil {
		// FIXME: do something with the error
		panic(err)
	}

	return nil
}

func (m *MockSystemImage) GetSetting(key string) (string, error) {
	return fmt.Sprintf("value-of: %s", key), nil
}

type SITestSuite struct {
	conn            *dbus.Connection
	mockSystemImage *MockSystemImage
	systemImage     *SystemImageRepository

	tmpdir      string
	mockService *DBusService

	privateDbusPid string
}

var _ = Suite(&SITestSuite{})

func (s *SITestSuite) SetUpSuite(c *C) {
	// setup our own private dbus session
	cmd := exec.Command("dbus-daemon", "--print-address", "--session", "--fork", "--print-pid")
	rawOutput, err := cmd.Output()
	c.Assert(err, IsNil)
	output := strings.Split(string(rawOutput), "\n")
	s.privateDbusPid = output[1]
	dbusEnv := output[0]
	os.Setenv("DBUS_SESSION_BUS_ADDRESS", dbusEnv)
}

func (s *SITestSuite) TearDownSuite(c *C) {
	cmd := exec.Command("kill", s.privateDbusPid)
	err := cmd.Run()
	c.Assert(err, IsNil)
}

func (s *SITestSuite) SetUpTest(c *C) {
	newPartition = func() (p partition.Interface) {
		return new(MockPartition)
	}

	var err error
	s.conn, err = dbus.Connect(dbus.SessionBus)
	c.Assert(err, IsNil)

	s.mockSystemImage = newMockSystemImage()
	s.mockService = NewDBusService(s.conn, systemImageInterface, systemImageObjectPath, systemImageBusName, s.mockSystemImage)
	c.Assert(s.mockService, NotNil)

	s.systemImage = newSystemImageRepositoryForBus(dbus.SessionBus)
	c.Assert(s, NotNil)
	// setup alternative root for system image
	tmpdir := c.MkDir()
	s.systemImage.myroot = tmpdir
	makeFakeSystemImageChannelConfig(c, filepath.Join(tmpdir, systemImageChannelConfig), "2.71")
	// setup fake /other partition
	makeFakeSystemImageChannelConfig(c, filepath.Join(tmpdir, "other", systemImageChannelConfig), "3.14")

	s.tmpdir = tmpdir
}

func (s *SITestSuite) TearDownTest(c *C) {
	os.RemoveAll(s.tmpdir)
	s.conn.Close()
}

func makeFakeSystemImageChannelConfig(c *C, cfgPath, buildNumber string) {
	os.MkdirAll(filepath.Dir(cfgPath), 0775)
	f, err := os.OpenFile(cfgPath, os.O_CREATE|os.O_RDWR, 0664)
	c.Assert(err, IsNil)
	defer f.Close()
	f.Write([]byte(fmt.Sprintf(`
[service]
base: system-image.ubuntu.com
http_port: 80
https_port: 443
channel: ubuntu-core/devel-proposed
device: generic_amd64
build_number: %s
version_detail: ubuntu=20141206,raw-device=20141206,version=77
`, buildNumber)))
}

func (s *SITestSuite) TestLowLevelGetSetting(c *C) {
	value, err := s.systemImage.proxy.GetSetting("all-cool")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "value-of: all-cool")
}

func (s *SITestSuite) TestLowLevelDownloadUpdate(c *C) {
	// add a update
	err := s.systemImage.proxy.DownloadUpdate()
	c.Assert(err, IsNil)
}

func (s *SITestSuite) TestTestInstalled(c *C) {
	// whats installed
	parts, err := s.systemImage.Installed()
	c.Assert(err, IsNil)
	// we have one active and one inactive
	c.Assert(len(parts), Equals, 2)
	c.Assert(parts[0].Name(), Equals, "ubuntu-core")
	c.Assert(parts[0].Version(), Equals, "2.71")
	c.Assert(parts[0].Hash(), Equals, "e09c13f68fccef3b2fe0f5c8ff5c61acf2173b170b1f2a3646487147690b0970ef6f2c555d7bcb072035f29ee4ea66a6df7f6bb320d358d3a7d78a0c37a8a549")
	c.Assert(parts[0].IsActive(), Equals, true)

	// second partition is not active and has a different version
	c.Assert(parts[1].IsActive(), Equals, false)
	c.Assert(parts[1].Version(), Equals, "3.14")
}

func (s *SITestSuite) TestUpdateNoUpdate(c *C) {
	s.mockSystemImage.fakeAvailableVersion = "2.71"
	parts, err := s.systemImage.Updates()
	c.Assert(err, IsNil)
	c.Assert(len(parts), Equals, 0)
}

func (s *SITestSuite) TestUpdateHasUpdate(c *C) {
	// add a update
	s.mockSystemImage.fakeAvailableVersion = "3.14"
	parts, err := s.systemImage.Updates()
	c.Assert(err, IsNil)
	c.Assert(len(parts), Equals, 1)
	c.Assert(parts[0].Name(), Equals, "ubuntu-core")
	c.Assert(parts[0].Version(), Equals, "3.14")
	c.Assert(parts[0].DownloadSize(), Equals, int64(1234))
}

type MockPartition struct {
	updateBootloaderCalled    bool
	markBootSuccessfulCalled  bool
	syncBootloaderFilesCalled bool
}

func (p *MockPartition) UpdateBootloader() (err error) {
	p.updateBootloaderCalled = true
	return nil
}

func (p *MockPartition) MarkBootSuccessful() (err error) {
	p.markBootSuccessfulCalled = true
	return nil
}
func (p *MockPartition) SyncBootloaderFiles() (err error) {
	p.syncBootloaderFilesCalled = true
	return nil
}
func (p *MockPartition) IsNextBootOther() bool {
	return false
}

func (p *MockPartition) RunWithOther(option partition.MountOption, f func(otherRoot string) (err error)) (err error) {
	return f("/other")
}

type MockProgressMeter struct {
	total    float64
	progress []float64
	finished bool
	spin     bool
}

func (m *MockProgressMeter) Start(total float64) {
	m.total = total
}
func (m *MockProgressMeter) Set(current float64) {
	m.progress = append(m.progress, current)
}
func (m *MockProgressMeter) Spin(msg string) {
	m.spin = true
}
func (m *MockProgressMeter) Write(buf []byte) (n int, err error) {
	return len(buf), err
}
func (m *MockProgressMeter) Finished() {
	m.finished = true
}

func (s *SITestSuite) TestSystemImagePartInstallUpdatesPartition(c *C) {
	// add a update
	s.mockSystemImage.fakeAvailableVersion = "3.14"
	parts, err := s.systemImage.Updates()

	sp := parts[0].(*SystemImagePart)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	pb := &MockProgressMeter{}
	err = sp.Install(pb)
	c.Assert(err, IsNil)
	c.Assert(mockPartition.updateBootloaderCalled, Equals, true)
	c.Assert(pb.total, Equals, 100.0)
	c.Assert(pb.finished, Equals, true)
	c.Assert(pb.progress, DeepEquals, []float64{20.0, 40.0, 60.0, 80.0, 100.0})
}

func (s *SITestSuite) TestSystemImagePartInstall(c *C) {
	// add a update
	s.mockSystemImage.fakeAvailableVersion = "3.14"
	parts, err := s.systemImage.Updates()

	sp := parts[0].(*SystemImagePart)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	err = sp.Install(nil)
	c.Assert(err, IsNil)
	c.Assert(mockPartition.updateBootloaderCalled, Equals, true)
}

func (s *SITestSuite) TestSystemImagePartSetActiveAlreadyActive(c *C) {
	parts, err := s.systemImage.Installed()

	sp := parts[0].(*SystemImagePart)
	c.Assert(sp.IsActive(), Equals, true)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	err = sp.SetActive()
	c.Assert(err, IsNil)
	c.Assert(mockPartition.updateBootloaderCalled, Equals, false)
}

func (s *SITestSuite) TestSystemImagePartSetActiveMakeActive(c *C) {
	parts, err := s.systemImage.Installed()

	sp := parts[1].(*SystemImagePart)
	c.Assert(sp.IsActive(), Equals, false)
	mockPartition := MockPartition{}
	sp.partition = &mockPartition

	err = sp.SetActive()
	c.Assert(err, IsNil)
	c.Assert(mockPartition.updateBootloaderCalled, Equals, true)
}
