# TelemAgent

![Go Report Card][grc]
[![License][LIC-BADGE]][LIC]

TelemAgent is an MQTT proxy that connects to the entire Canonical Telemetry cloud. 

It is deployed in front of an MQTT broker and can be used for topic translation, user property injection, permission control, logging and debugging and various other purposes.

## Building TelemAgent
First, you need to ensure that you have the appropriate tools installed:

```bash
$ sudo snap install lxd
$ sudo snap install snapcraft --classic
$ lxd init --auto
```

### Snap
The first option is to install the TelemAgent as a snap. To do so, we use snapcraft

```bash
$ snapcraft 
telem-agent_0.1_amd64.snap has been created
$ snap install telem-agent_0.1_amd64.snap --dangerous
telem-agent installed
```
After building, there are some interfaces that need to be connected.
```bash
$ snap connect telem-agent:ssl  
$ snap connect telem-agent:system-observe  
$ snap connect telem-agent:network  
$ snap connect telem-agent:network-bind
$ snap connect telem-agent:snapd-control
$ snap connect telem-agent:topic-control
```
### Snap Component
The second option is to install the TelemAgent as a snap component for snapd. To do so, we use snapcraft

```bash
$ snapcraft pack . 
snapd+telem-agent_0.0.comp has been created
$ snap install snapd+telem-agent_0.0.comp --dangerous
snapd+telem-agent installed
```


<!-- ## Architecture -->

TelemAgent starts a TCP server, offering connections to devices. Upon the connection, it establishes a session with a remote MQTT broker (a component of Canonical Telemetry). It then pipes packets from devices to the MQTT broker, inspecting or modifying them as they flow through the proxy.

Here is the flow in more detail:

- The Device connects to TelemAgent's TCP server
- TelemAgent accepts the inbound (IN) connection and establishes a new session with the remote MQTT broker (i.e. it dials out to the MQTT broker only once it accepts a new connection from a device. This way one device-TelemAgent connection corresponds to one TelemAgent-MQTT broker connection.)
- TelemAgent then spawns 2 goroutines: one that will read incoming packets from the device-TelemAgent socket (INBOUND or UPLINK), inspect them (calling event handlers) and write them to TelemAgent-broker socket (forwarding them towards the broker) and other that will be reading MQTT broker responses from TelemAgent-broker socket and writing them towards device, in device-TelemAgent socket (OUTBOUND or DOWNLINK).

<!-- <p align="center"><img src="docs/img/mproxy.png"></p> -->

TelemAgent can parse and understand MQTT packages, and upon their detection, it calls external event handlers. Event handlers should implement the following interface defined in [pkg/mqtt/events.go](pkg/mqtt/events.go):

```go
// Handler is an interface for TelemAgent hooks
type Handler interface {
    // Authorization on client `CONNECT`
    // Each of the params are passed by reference, so that it can be changed
    AuthConnect(ctx context.Context) error

    // Authorization on client `PUBLISH`
    // Topic is passed by reference, so that it can be modified
    AuthPublish(ctx context.Context, topic *string, payload *[]byte) error

    // Authorization on client `SUBSCRIBE`
    // Topics are passed by reference, so that they can be modified
    AuthSubscribe(ctx context.Context, topics *[]string) error

    // Reconvert topics on client going down
    // Topics are passed by reference, so that they can be modified
    DownPublish(ctx context.Context, topic *string, userProperties *[]packets.User) error

    // After client successfully connected
    Connect(ctx context.Context)

    // After client successfully published
    Publish(ctx context.Context, topic *string, payload *[]byte)

    // After client successfully subscribed
    Subscribe(ctx context.Context, topics *[]string)

    // After client unsubscribed
    Unsubscribe(ctx context.Context, topics *[]string)

    // Disconnect on connection with client lost
    Disconnect(ctx context.Context)
}
```

An example of implementation is given [here](handlers/simple/simple.go), alongside with it's [`main()` function](cmd/main.go). The primary handler used at the moment is the [snapadder](handlers/snapadder/snapadder.go).

## TLS Certificates

TelemAgent runs its connections over TLS for both snap-side and broker-side communications. To do so, there needs to be a certificate present for clients to authenticate that TelemAgent is a trusted authority. We currently have some self-signed certificates in the `/ssl` directory which can be used for development purposes.

## Example Setup & Testing of TelemAgent

### Requirements

- Golang
- Mosquitto MQTT Server
- Mosquitto Publisher and Subscriber Client

### Example Setup of TelemAgent

TelemAgent is used to proxy requests to a backend server. For the example setup, we will use Mosquitto server as the backend for MQTT.

1. Start the Mosquitto MQTT Server with the following command. This bash script will initiate the Mosquitto MQTT server. The Mosquitto Server will listen for MQTT connections over TLSon port 1883.

   ```bash
   examples/server/mosquitto/server.sh
   ```

2. Start the example TelemAgent servers for various protocols:

   ```bash
   go run cmd/main.go
   ```

   The `cmd/main.go` Go program initializes TelemAgent servers for the following protocols:

   - TelemAgent server for `MQTT` protocol `without TLS` on port `1884`
   - TelemAgent server for `MQTT` protocol `with TLS` on port `8883`

### Example testing of TelemAgent

#### Test TelemAgent server for MQTT protocols

Bash scripts available in `examples/client/mqtt` directory help to test the TelemAgent servers running for MQTT protocols

- Script to test TelemAgent server running at port 1884 for MQTT without TLS

  ```bash
  examples/client/mqtt/without_tls.sh
  ```

- Script to test TelemAgent server running at port 8883 for MQTT with TLS

  ```bash
  examples/client/mqtt/with_tls.sh
  ```

- Script to test TelemAgent server running at port 8884 for MQTT with mTLS

  ```bash
  examples/client/mqtt/with_mtls.sh
  ```

## Configuration

The service is configured using the environment variables presented in the following table. Note that any unset variables will be replaced with their default values.

| Variable                                           | Description                                                                                                                           | Default                      |
| -------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------- |
| MPROXY_MQTT_WITHOUT_TLS_ADDRESS                    | MQTT without TLS inbound (IN) connection listening address                                                                            | :1884                        |
| MPROXY_MQTT_WITHOUT_TLS_TARGET                     | MQTT without TLS outbound (OUT) connection address                                                                                    | localhost:1883               |
| MPROXY_MQTT_WITH_TLS_ADDRESS                       | MQTT with TLS inbound (IN) connection listening address                                                                               | :8883                        |
| MPROXY_MQTT_WITH_TLS_TARGET                        | MQTT with TLS outbound (OUT) connection address                                                                                       | localhost:1883               |
| MPROXY_MQTT_WITH_TLS_CERT_FILE                     | MQTT with TLS certificate file path                                                                                                   | ssl/certs/server.crt         |
| MPROXY_MQTT_WITH_TLS_KEY_FILE                      | MQTT with TLS key file path                                                                                                           | ssl/certs/server.key         |
| MPROXY_MQTT_WITH_TLS_SERVER_CA_FILE                | MQTT with TLS server CA file path                                                                                                     | ssl/certs/ca.crt             |

## TelemAgent Configuration Environment Variables

### Server Configuration Environment Variables

- `ADDRESS` : Specifies the address at which TelemAgent will listen. Supports MQTT, MQTT over WebSocket, and HTTP proxy connections.
- `PATH_PREFIX` : Defines the path prefix when listening for MQTT over WebSocket or HTTP connections.
- `TARGET` : Specifies the address of the target server, including any prefix path if available. The target server can be an MQTT server, MQTT over WebSocket, or an HTTP server.

### TLS Configuration Environment Variables

- `CERT_FILE` : Path to the TLS certificate file.
- `KEY_FILE` : Path to the TLS certificate key file.
- `SERVER_CA_FILE` : Path to the Server CA certificate file.
- `CLIENT_CA_FILE` : Path to the Client CA certificate file.

## Adding Prefix to Environmental Variables

TelemAgent relies on the [caarlos0/env](https://github.com/caarlos0/env) package to load environmental variables into its [configuration](https://github.com/canonical/telem-agent/blob/main/config/config.go#L13).
You can control how these variables are loaded by passing `env.Options` to the `config.EnvParse` function.

To add a prefix to environmental variables, use `env.Options{Prefix: "MPROXY_"}` from the [caarlos0/env](https://github.com/caarlos0/env) package. For example:

```go
package main
import (
  "github.com/caarlos0/env/v11"
  "github.com/absmach/mproxy"
)

mqttConfig := mproxy.Config{}
if err := mqttConfig.EnvParse(env.Options{Prefix:  "MPROXY_" }); err != nil {
    panic(err)
}
fmt.Printf("%+v\n")
```

In the above snippet, `mqttConfig.EnvParse` expects all environmental variables with the prefix `MPROXY_`.
For instance:

- MPROXY_ADDRESS
- MPROXY_PATH_PREFIX
- MPROXY_TARGET
- MPROXY_CERT_FILE
- MPROXY_KEY_FILE
- MPROXY_SERVER_CA_FILE
- MPROXY_CLIENT_CA_FILE

## License

[Apache-2.0](LICENSE)

[grc]: https://goreportcard.com/badge/github.com/absmach/mproxy
[LIC]: LICENCE
[LIC-BADGE]: https://img.shields.io/badge/License-Apache_2.0-blue.svg
