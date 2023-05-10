package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/godbus/dbus"
	"github.com/godbus/dbus/introspect"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/prompting/notifier"
	"github.com/snapcore/snapd/prompting/storage"
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

	decisions *storage.PromptsDB
}

func NewPromptNotifierDbus() (*PromptNotifierDbus, error) {
	notifier, err := notifier.Register()
	if err != nil {
		return nil, err
	}

	dbusNotifier := &PromptNotifierDbus{
		notifier:  notifier,
		agents:    make(map[uint32]agentAddr),
		decisions: storage.New(),
	}
	if err := dbusNotifier.setupDbus(); err != nil {
		return nil, err
	}

	return dbusNotifier, nil
}

// maybeWorkaroundMissingDBusPolicy will inspect an error to see if it
// indicates that the dbus policy file is not installed and if so fix
// that
//
// XXX: the deb really needs to take care of this
func maybeWorkaroundMissingDBusPolicy(dbusErr dbus.Error) error {
	if dbusErr.Name != "org.freedesktop.DBus.Error.AccessDenied" {
		return dbusErr
	}
	if len(dbusErr.Body) < 1 {
		return dbusErr
	}
	message, ok := dbusErr.Body[0].(string)
	if !ok {
		return dbusErr
	}
	// fugly but dbus bus errors are not translaed and it it seems the
	// only way
	if !strings.Contains(message, "is not allowed to own the service") {
		return dbusErr
	}

	// At this point it very much looks like the service cannot start
	// because the dbus policy for io.snapcraft.AppArmorPrompt is not
	// installed (probably because of a snapd that re-execs). Workaround
	// this here.
	// XXX: is this what we want long term?
	src := "/snap/snapd/current/usr/share/dbus-1/system.d/io.snapcraft.AppArmorPrompt.conf"
	dst := "/usr/share/dbus-1/system.d/io.snapcraft.AppArmorPrompt.conf"
	if osutil.FileExists(dst) {
		return dbusErr
	}
	logger.Noticef("installing dbus config %v -> %v", src, dst)
	if err := osutil.CopyFile(src, dst, 0); err != nil {
		return err
	}

	return nil
}

func (p *PromptNotifierDbus) setupDbus() error {
	// godbus uses a global systemBus object internally so we *must*
	// not close the connection.
	conn, err := dbusutil.SystemBus()
	if err != nil {
		return err
	}

	reply, err := conn.RequestName("io.snapcraft.AppArmorPrompt", dbus.NameFlagDoNotQueue)
	if dbusErr, ok := err.(dbus.Error); ok {
		if err := maybeWorkaroundMissingDBusPolicy(dbusErr); err != nil {
			return err
		}
		// give dbus time to pickup the changed config via inotify
		time.Sleep(1 * time.Second)
		// try to get the name again
		reply, err = conn.RequestName("io.snapcraft.AppArmorPrompt", dbus.NameFlagDoNotQueue)
	}
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
	// we have some stored allow/deny decisions already
	if yesNo, err := p.decisions.Get(req); err == nil {
		req.YesNo <- yesNo
		return
	}

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
	if err := p.decisions.Set(req, resAllowed, resExtra); err != nil {
		logger.Noticef("cannot store prompt decision: %v", err)
	}
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
			logger.Noticef("Got from kernel req ch %v", req)
			// XXX: deal with the kernel timeout of prompts
			go p.handleReq(req)

		case err := <-p.notifier.E:
			logger.Noticef("Got from kernel error ch %v", err)
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
