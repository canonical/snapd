package builtin

const bcacheSummary = `allows access to bcache kernel interfaces`

const bcacheBaseDeclarationSlots = `
  bcache:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const bcacheConnectedPlugAppArmor = `
# Description: Can access kernel bcache interfaces in sysfs

# Allow read/write access to kernel bcache interfaces in sysfs
# includes items in /sys/fs/bcache, /sys/block and /sys/devices
/sys**bcache** rwk,
`

func init() {
	registerIface(&commonInterface{
		name:                  "bcache",
		summary:               bcacheSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  bcacheBaseDeclarationSlots,
		connectedPlugAppArmor: bcacheConnectedPlugAppArmor,
	})
}
