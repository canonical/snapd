package assemblestate

type Auth struct {
	HMAC []byte `json:"hmac"`
	RDT  RDT    `json:"rdt"`
}

type UnknownDevices struct {
	Devices []RDT `json:"devices"`
}

type Routes struct {
	Devices   []RDT    `json:"devices"`
	Addresses []string `json:"addresses"`
	Routes    []int    `json:"routes"`
}

type Devices struct {
	Devices []Identity `json:"devices"`
}

type (
	FP    [64]byte
	Proof [64]byte
	RDT   string
)

type Identity struct {
	RDT RDT `json:"rdt"`

	// TODO: we're not using these yet, but we eventually will. not critical to
	// the core of the protocol really.
	FP          FP     `json:"fp"`
	Serial      string `json:"serial"`
	SerialProof Proof  `json:"serial-proof"`
}
