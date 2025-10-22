package hooks

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"math/rand"
	"net/url"
	"strconv"

	"github.com/snapcore/snapd/telemagent/pkg/utils"

	mptls "github.com/snapcore/snapd/telemagent/pkg/tls"

	"github.com/caarlos0/env/v11"
	"github.com/canonical/mqtt.golang/autopaho"
	"github.com/canonical/mqtt.golang/paho"
	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"github.com/snapcore/snapd/client"
)

const DeniedTopic = "DENIED"
const ErrorTopic = "ERROR"

type Config struct {
	Enabled    bool   `env:"ENABLED"      envDefault:"false"`
	Endpoint   string `env:"ENDPOINT"     envDefault:"mqtt://localhost:1883"`
	ServerPort int    `env:"PORT"         envDefault:"9090"`
	BrokerPort string `env:"BROKER_PORT"  envDefault:":1884"`
	Email      string `env:EMAIL`
	TLSConfig  *tls.Config
}

// Options contains configuration settings for the hook.
type TelemAgentHookOptions struct {
	Server     *mochi.Server
	mqttConfig autopaho.ClientConfig
	mqttClient *autopaho.ConnectionManager
	router     paho.Router
	Cfg        Config
}

type TelemAgentHook struct {
	mochi.HookBase
	config *TelemAgentHookOptions
}

func NewConfig(opts env.Options) (Config, error) {
	c := Config{}
	if err := env.ParseWithOptions(&c, opts); err != nil {
		return Config{}, err
	}

	cfg, err := mptls.NewConfig(opts)
	if err != nil {
		return Config{}, err
	}

	c.TLSConfig, err = mptls.Load(&cfg)
	if err != nil {
		return Config{}, err
	}

	return c, nil
}

func (h *TelemAgentHook) Init(config any) error {
	h.Log.Info("initialised")
	if _, ok := config.(*TelemAgentHookOptions); !ok && config != nil {
		return mochi.ErrInvalidConfigType
	}

	h.config = config.(*TelemAgentHookOptions)
	if h.config.Server == nil {
		return mochi.ErrInvalidConfigType
	}

	snapClient := client.New(nil)

	macaroon, err := snapClient.DeviceSession()
	if err != nil {
		h.Log.Error(err.Error())
	}

	deviceID, err := utils.GetDeviceId()
	if err != nil {
		h.Log.Warn(err.Error())
	}

	h.Log.Info(fmt.Sprintf("Acquired device session macaroon: %s", macaroon[0]))

	u, err := url.Parse(h.config.Cfg.Endpoint)
	if err != nil {
		return err
	}

	router := paho.NewStandardRouter()

	cliCfg := autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{u},
		KeepAlive:                     20,
		CleanStartOnInitialConnection: false,
		SessionExpiryInterval:         60,
		TlsCfg:                        h.config.Cfg.TLSConfig,
		OnConnectionUp: func(cm *autopaho.ConnectionManager, connAck *paho.Connack) {
			h.Log.Info(fmt.Sprintf("Server connected to MQTT broker on address %s", h.config.Cfg.Endpoint))
		},
		OnConnectError:  func(err error) { h.Log.Error(fmt.Sprintf("error whilst attempting connection: %s\n", err)) },
		ConnectUsername: deviceID,
		ConnectPassword: []byte(macaroon[0]),
		ConnectPacketBuilder: func(cp *paho.Connect, u *url.URL) (*paho.Connect, error) {
			if cp.Properties == nil {
				cp.Properties = &paho.ConnectProperties{}
			}
			cp.Properties.User = append(cp.Properties.User, paho.UserProperty{Key: "client-type", Value: "device"})
			return cp, nil
		},
		ClientConfig: paho.ClientConfig{
			ClientID:      deviceID + "-" + strconv.Itoa(1e4+rand.Int()%9e4),
			OnClientError: func(err error) { h.Log.Error(fmt.Sprintf("client error: %s\n", err)) },
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){
				func(p paho.PublishReceived) (bool, error) {
					h.config.router.Route(p.Packet.Packet())
					return false, nil
				},
			},
			OnServerDisconnect: func(d *paho.Disconnect) {
				h.Log.Info(fmt.Sprintf("server requested disconnect; reason code: %d\n", d.ReasonCode))
			},
		},
	}

	h.config.mqttConfig = cliCfg
	h.config.router = router
	return nil
}

func (h *TelemAgentHook) ID() string {
	return "detect-snap"
}

func (h *TelemAgentHook) Provides(b byte) bool {
	return bytes.Contains([]byte{
		mochi.OnConnectAuthenticate,
		mochi.OnSubscribe,
		mochi.OnACLCheck,
		mochi.OnPublish,
		mochi.OnPacketEncode,
		mochi.OnStarted,
	}, []byte{b})
}

func (h *TelemAgentHook) OnConnectAuthenticate(cl *mochi.Client, pk packets.Packet) bool {

	snapPublisher, snapName, err := utils.GetSnapInfoFromConn(cl.Net.Conn.RemoteAddr().String())

	if err != nil {
		h.Log.Error(fmt.Sprintf("failed to get snap info: %v", err))
		return false
	}

	h.Log.Info(fmt.Sprintf("receieved packet from snap %s - %s", snapName, snapPublisher))

	return true
}
