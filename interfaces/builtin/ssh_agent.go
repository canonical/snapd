// -*- Mode: Go; indent-tabs-mode: t -*-

package builtin

const sshAgentSummary = `allows access to users ssh-agent`
const sshAgentBaseDeclarationSlots = `
  ssh-agent:
    allow-installation:
      slot-snap-type:
        - app
    deny-auto-connection: true
`

const sshAgentConnectedPlugAppArmor = `
# allow access to socket owned by user in default locatin for openssh ssh-agent
owner /tmp/ssh-[a-zA-Z0-9]+/agent.[0-9]+ rw,
# allow access to default location for gnome keyring ssh-agent for standard users (uid 1000+)
owner /run/user/[0-9]{4,}/keyring/ssh rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "ssh-agent",
		summary:               sshAgentSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  sshAgentBaseDeclarationSlots,
		connectedPlugAppArmor: sshAgentConnectedPlugAppArmor,
	})
}
