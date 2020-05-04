// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package builtin

import (
	"errors"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

const hwmonControlSummary = `allows monitoring of system hardware`

const hwmonControlBaseDeclarationSlots = `
  hwmon-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const hwmonAppArmorPath = `/sys/devices/**/hwmon[0-9]*`

const hwmonControlConnectedPlugAppArmor = `
# Description: This interface allows for monitoring hardware and platform
# devices connected to the system.
# This is reserved because it allows potentially disruptive operations and
# access to devices which may contain sensitive information.

# https://www.kernel.org/doc/Documentation/hwmon/sysfs-interface
# hwmon - common accesses
/run/udev/data/+hwmon:hwmon[0-9]* r,
/sys/class/hwmon/ r,
HWMON/ r,
HWMON/name r,
HWMON/update_interval r,
#HWMON/update_interval w, # snapd needs dynamic detection to mediate writes

# alarms
HWMON/beep_enable r,
#HWMON/beep_enable w, # snapd needs dynamic detection to mediate writes

# Deprecated for old drivers using a non-standard interface to alarms
# and beeps.
#HWMON/alarms r,
#HWMON/beep_mask r,
#HWMON/beep_mask rw, # snapd needs dynamic detection to mediate writes
`

const hwmonAppArmorChannelCurrent = `
# hwmon - current
HWMON/curr[1-9]*_max rw,
HWMON/curr[1-9]*_min rw,
HWMON/curr[1-9]*_lcrit rw,
HWMON/curr[1-9]*_crit rw,
HWMON/curr[1-9]*_input r,
HWMON/curr[1-9]*_average r,
HWMON/curr[1-9]*_lowest r,
HWMON/curr[1-9]*_highest r,
HWMON/curr[1-9]*_reset_history w,
HWMON/curr[1-9]*_enable rw,
# alarms
HWMON/curr[1-9]*_alarm r,
HWMON/curr[1-9]*_min_alarm r,
HWMON/curr[1-9]*_max_alarm r,
HWMON/curr[1-9]*_lcrit_alarm r,
HWMON/curr[1-9]*_crit_alarm r,
HWMON/curr[1-9]*_beep rw,
`

const hwmonAppArmorChannelEnergy = `
# hwmon - energy
HWMON/energy[1-9]*_input r,
HWMON/energy[1-9]*_enable rw,
`

const hwmonAppArmorChannelFan = `
# hwmon - fan
HWMON/fan[1-9]*_min rw,
HWMON/fan[1-9]*_max rw,
HWMON/fan[1-9]*_input r,
HWMON/fan[1-9]*_div rw,
HWMON/fan[1-9]*_pulses rw,
HWMON/fan[1-9]*_target rw,
HWMON/fan[1-9]*_label r,
HWMON/fan[1-9]*_enable rw,
# alarms
HWMON/fan[1-9]*_alarm r,
HWMON/fan[1-9]*_min_alarm r,
HWMON/fan[1-9]*_max_alarm r,
HWMON/fan[1-9]*_beep rw,
# faults
HWMON/fan[1-9]*_fault r,
`

const hwmonAppArmorChannelHumidity = `
# hwmon - humidity
HWMON/humidity[1-9]*_input r,
HWMON/humidity[1-9]*_enable rw,
`

const hwmonAppArmorChannelIntrusion = `
# hwmon - intrusion
HWMON/intrusion[0-9]*_alarm rw,
HWMON/intrusion[0-9]*_beep rw,
`

const hwmonAppArmorChannelPower = `
# hwmon - power
HWMON/power[1-9]*_average r,
HWMON/power[1-9]*_average_interval rw,
HWMON/power[1-9]*_average_interval_max r,
HWMON/power[1-9]*_average_interval_min r,
HWMON/power[1-9]*_average_highest r,
HWMON/power[1-9]*_average_lowest r,
HWMON/power[1-9]*_average_max rw,
HWMON/power[1-9]*_average_min rw,
HWMON/power[1-9]*_input r,
HWMON/power[1-9]*_input_highest r,
HWMON/power[1-9]*_input_lowest r,
HWMON/power[1-9]*_reset_history w,
HWMON/power[1-9]*_accuracy r,
HWMON/power[1-9]*_cap rw,
HWMON/power[1-9]*_cap_hyst rw,
HWMON/power[1-9]*_cap_max r,
HWMON/power[1-9]*_cap_min r,
HWMON/power[1-9]*_max rw,
HWMON/power[1-9]*_crit rw,
HWMON/power[1-9]*_enable rw,
# alarms
HWMON/power[1-9]*_alarm r,
HWMON/power[1-9]*_cap_alarm r,
HWMON/power[1-9]*_max_alarm r,
HWMON/power[1-9]*_crit_alarm r,
`

const hwmonAppArmorChannelPwm = `
# hwmon - pwm
HWMON/pwm[1-9]* rw,
HWMON/pwm[1-9]*_enable rw,
HWMON/pwm[1-9]*_mode rw,
HWMON/pwm[1-9]*_freq rw,
HWMON/pwm[1-9]*_auto_channels_temp rw,
HWMON/pwm[1-9]*_auto_point[1-9]*_pwm rw,
HWMON/pwm[1-9]*_auto_point[1-9]*_temp rw,
HWMON/pwm[1-9]*_auto_point[1-9]*_temp_hyst rw,
HWMON/temp[1-9]*_auto_point[1-9]*_pwm rw,
HWMON/temp[1-9]*_auto_point[1-9]*_temp rw,
HWMON/temp[1-9]*_auto_point[1-9]*_temp_hyst rw,
`

const hwmonAppArmorChannelTemperature = `
# hwmon - temperature
HWMON/temp[1-9]*_type rw,
HWMON/temp[1-9]*_max rw,
HWMON/temp[1-9]*_min rw,
HWMON/temp[1-9]*_max_hyst rw,
HWMON/temp[1-9]*_min_hyst rw,
HWMON/temp[1-9]*_input r,
HWMON/temp[1-9]*_crit rw,
HWMON/temp[1-9]*_crit_hyst rw,
HWMON/temp[1-9]*_emergency rw,
HWMON/temp[1-9]*_emergency_hyst rw,
HWMON/temp[1-9]*_lcrit rw,
HWMON/temp[1-9]*_lcrit_hyst rw,
HWMON/temp[1-9]*_offset rw,
HWMON/temp[1-9]*_label r,
HWMON/temp[1-9]*_lowest r,
HWMON/temp[1-9]*_highest r,
HWMON/temp[1-9]*_reset_history w,
HWMON/temp[1-9]*_enable rw,
# alarms
HWMON/temp[1-9]*_alarm r,
HWMON/temp[1-9]*_min_alarm r,
HWMON/temp[1-9]*_max_alarm r,
HWMON/temp[1-9]*_lcrit_alarm r,
HWMON/temp[1-9]*_crit_alarm r,
HWMON/temp[1-9]*_emergency_alarm r,
HWMON/temp[1-9]*_beep rw,
# faults
HWMON/temp[1-9]*_fault r,
`

const hwmonAppArmorChannelVoltage = `
# hwmon - voltage
HWMON/in[0-9]*_min rw,
HWMON/in[0-9]*_max rw,
HWMON/in[0-9]*_crit rw,
HWMON/in[0-9]*_lcrit rw,
HWMON/in[0-9]*_input r,
HWMON/in[0-9]*_average r,
HWMON/in[0-9]*_lowest r,
HWMON/in[0-9]*_highest r,
HWMON/in[0-9]*_reset_history w,
HWMON/in[0-9]*_label r,
HWMON/in[0-9]*_enable rw,
HWMON/cpu[0-9]*_vid r,
HWMON/vrm rw,
# alarms
HWMON/in[0-9]*_alarm r,
HWMON/in[0-9]*_min_alarm r,
HWMON/in[0-9]*_max_alarm r,
HWMON/in[0-9]*_lcrit_alarm r,
HWMON/in[0-9]*_crit_alarm r,
HWMON/in[0-9]*_beep rw,
`

var hwmonChannelsAttributeError = errors.New(`hwmon-control "channels" attribute must be a list of strings`)

var hwmonControlApparmorChannelSnippets = map[string]string{
	"current":     hwmonAppArmorChannelCurrent,
	"energy":      hwmonAppArmorChannelEnergy,
	"fan":         hwmonAppArmorChannelFan,
	"humidity":    hwmonAppArmorChannelHumidity,
	"intrusion":   hwmonAppArmorChannelIntrusion,
	"power":       hwmonAppArmorChannelPower,
	"pwm":         hwmonAppArmorChannelPwm,
	"temperature": hwmonAppArmorChannelTemperature,
	"voltage":     hwmonAppArmorChannelVoltage,
}

type hwmonControlInterface struct {
	commonInterface
}

func hwmonChannels(plug interfaces.Attrer) ([]string, error) {
	channelsAttr, ok := plug.Lookup("channels")
	if !ok {
		return nil, nil
	}
	channelInterfaces, ok := channelsAttr.([]interface{})
	if !ok {
		return nil, hwmonChannelsAttributeError
	}

	channels := make([]string, 0, len(channelInterfaces))
	for _, c := range channelInterfaces {
		channel, ok := c.(string)
		if !ok {
			return nil, hwmonChannelsAttributeError
		}

		channels = append(channels, channel)
	}

	return channels, nil
}

func (iface *hwmonControlInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	channels, err := hwmonChannels(plug)
	if err != nil {
		return err
	}

	for _, channel := range channels {
		if _, ok := hwmonControlApparmorChannelSnippets[channel]; !ok {
			return fmt.Errorf(`hwmon-control: unsupported "channels" attribute %q`, channel)
		}
	}
	return nil
}

func (iface *hwmonControlInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	channels, err := hwmonChannels(plug)
	if err != nil {
		return err
	}

	spec.AddSnippet(strings.Replace(hwmonControlConnectedPlugAppArmor, "HWMON", hwmonAppArmorPath, -1))

	for channel, snippet := range hwmonControlApparmorChannelSnippets {
		// If the list of channels is not empty, we must consider only the
		// channels in the list
		if len(channels) > 0 && !strutil.ListContains(channels, channel) {
			continue
		}

		spec.AddSnippet(strings.Replace(snippet, "HWMON", hwmonAppArmorPath, -1))
	}

	return nil
}

func init() {
	registerIface(&hwmonControlInterface{
		commonInterface: commonInterface{
			name:                  "hwmon-control",
			summary:               hwmonControlSummary,
			implicitOnCore:        true,
			implicitOnClassic:     true,
			baseDeclarationSlots:  hwmonControlBaseDeclarationSlots,
			connectedPlugAppArmor: hwmonControlConnectedPlugAppArmor,
		},
	})
}
