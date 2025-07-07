package assemblestate

type Auth struct {
	HMAC []byte      `json:"hmac"`
	RDT  DeviceToken `json:"rdt"`
}

type UnknownDevices struct {
	Devices []DeviceToken `json:"devices"`
}

type Routes struct {
	Devices   []DeviceToken `json:"devices"`
	Addresses []string      `json:"addresses"`
	Routes    []int         `json:"routes"`
}

type Devices struct {
	Devices []Identity `json:"devices"`
}

type (
	Fingerprint [64]byte
	Proof       [64]byte
	DeviceToken string
)

type Identity struct {
	RDT DeviceToken `json:"rdt"`

	// TODO: we're not using these yet, but we eventually will. not critical to
	// the core of the protocol really.
	FP          Fingerprint `json:"fp"`
	Serial      string      `json:"serial"`
	SerialProof Proof       `json:"serial-proof"`
}
