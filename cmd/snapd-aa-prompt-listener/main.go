package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/godbus/dbus"
	"github.com/godbus/dbus/introspect"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/prompting/notifier"
	"github.com/snapcore/snapd/snapdtool"
)

func senderUid(conn *dbus.Conn, sender dbus.Sender) (uint32, error) {
	obj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	var creds map[string]dbus.Variant
	err := obj.Call("org.freedesktop.DBus.GetConnectionCredentials", 0, sender).Store(&creds)
	if err != nil {
		return 0, err
	}
	v := creds["UnixUserID"].Value()
	remoteUid, ok := v.(uint32)
	if !ok {
		return 0, fmt.Errorf("unknown type for %v (%T)", v, v)
	}
	return remoteUid, nil
}

const aaPromptIntrospectionData = `
<node>
	<interface name="io.snapcraft.AppArmorPrompt">
		<method name="RegisterAgent">
                    <arg name="path" direction="in" type="s"/>
		</method>
	</interface>` + introspect.IntrospectDataString + `</node> `

type promptRoot struct {
	owner *PromptNotifierDbus
}

// TODO: add "locale string" and "capabilities map[string]string" here
// for future compatbilities
func (pr *promptRoot) RegisterAgent(sender dbus.Sender, path dbus.ObjectPath) *dbus.Error {
	uid, err := senderUid(pr.owner.conn, sender)
	if err != nil {
		return dbus.MakeFailedError(err)
	}
	pr.owner.agents[uid] = agentAddr{string(sender), path}
	logger.Debugf("RegisterAgent: agent registered for uid %v at (%v, %v)", uid, sender, path)

	return nil
}

type agentAddr struct {
	uniqeName string
	path      dbus.ObjectPath
}

type PromptNotifierDbus struct {
	notifier *notifier.Notifier
	conn     *dbus.Conn

	// agent for uid
	agents map[uint32]agentAddr
}

func NewPromptNotifierDbus() (*PromptNotifierDbus, error) {
	notifier, err := notifier.Register()
	if err != nil {
		return nil, err
	}

	dbusNotifier := &PromptNotifierDbus{
		notifier: notifier,
		agents:   make(map[uint32]agentAddr),
	}
	if err := dbusNotifier.setupDbus(); err != nil {
		return nil, err
	}

	return dbusNotifier, nil
}

func (p *PromptNotifierDbus) setupDbus() error {
	// godbus uses a global systemBus object internally so we *must*
	// not close the connection.
	conn, err := dbusutil.SystemBus()
	if err != nil {
		return err
	}

	reply, err := conn.RequestName("io.snapcraft.AppArmorPrompt", dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("cannot setup prompt: dbus name already taken")
	}

	p.conn = conn
	return nil
}

func (p *PromptNotifierDbus) handleReq(req *notifier.Request) {
	uid := req.SubjectUid
	agent, ok := p.agents[uid]
	if !ok {
		logger.Noticef("no agent registered for uid %v", uid)
		req.YesNo <- false
		return
	}

	info := map[string]interface{}{
		"pid": req.Pid,
		// XXX: aa-label?
		"label":      req.Label,
		"permission": req.Permission,
	}

	obj := p.conn.Object(agent.uniqeName, agent.path)
	var resAllowed bool
	var resExtra map[string]string
	if err := obj.Call("io.snapcraft.PromptAgent.Prompt", 0, req.Path, info).Store(&resAllowed, &resExtra); err != nil {
		logger.Noticef("cannot call prompt agent for %v: %v", uid, err)
		return
	}
	logger.Debugf("got result: %v (%v)", resAllowed, resExtra)
	req.YesNo <- resAllowed
}

func (p *PromptNotifierDbus) Run() error {
	go p.notifier.Run()

	dbusName := dbus.ObjectPath("/io/snapcraft/AppArmorPrompt")
	if err := p.conn.Export(&promptRoot{owner: p}, dbusName, "io.snapcraft.AppArmorPrompt"); err != nil {
		return err
	}
	if err := p.conn.Export(introspect.Introspectable(aaPromptIntrospectionData), dbusName, "org.freedesktop.DBus.Introspectable"); err != nil {
		logger.Noticef("cannot export introspection data: %v", err)
	}

	logger.Noticef("ready for prompts")
	for {
		logger.Debugf("waiting prompt loop")
		select {
		case req := <-p.notifier.R:
			logger.Debugf("req ch %v", req)
			// XXX: deal with the kernel timeout of prompts
			go p.handleReq(req)

		case err := <-p.notifier.E:
			logger.Debugf("err ch %v", err)
			return err
		}
	}
}

func (p *PromptNotifierDbus) Close() error {
	if err := p.notifier.Close(); err != nil {
		logger.Noticef("cannot close notifier: %v", err)
		return err
	}
	return nil
}

func run() error {
	logger.Noticef("starting agent")
	dbusNotifier, err := NewPromptNotifierDbus()
	if err != nil {
		return err
	}
	defer dbusNotifier.Close()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Reset(os.Interrupt)
	go func() {
		sig := <-signals
		log.Printf("Got signal %s\n", sig)
		go dbusNotifier.Close()
	}()

	return dbusNotifier.Run()
}

func main() {
	if err := logger.SimpleSetup(); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to activate logging: %v\n", err)
	}
	snapdtool.ExecInSnapdOrCoreSnap()

	if err := run(); err != nil {
		log.Fatalf("error: %s\n", err)
	}
}
