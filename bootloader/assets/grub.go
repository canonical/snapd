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

package assets

// TODO:UC20 extract common and template parts of command line

// scripts content from https://github.com/snapcore/pc-amd64-gadget, commit:
//
// commit e4d63119322691f14a3f9dfa36a3a075e941ec9d (HEAD -> 20, origin/HEAD, origin/20)
// Merge: b70d2ae d113aca
// Author: Dimitri John Ledkov <xnox@ubuntu.com>
// Date:   Thu May 7 19:30:00 2020 +0100
//
//     Merge pull request #47 from xnox/production-keys
//
//     gadget: bump edition to 2, using production signing keys for everything.

const grubBootConfig = `# Snapd-Boot-Config-Edition: 1

set default=0
set timeout=3
set timeout_style=hidden

# load only kernel_status from the bootenv
load_env --file /EFI/ubuntu/grubenv kernel_status snapd_extra_cmdline_args

set snapd_static_cmdline_args='console=ttyS0 console=tty1 panic=-1'

set kernel=kernel.efi

if [ "$kernel_status" = "try" ]; then
    # a new kernel got installed
    set kernel_status="trying"
    save_env kernel_status

    # use try-kernel.efi
    set kernel=try-kernel.efi
elif [ "$kernel_status" = "trying" ]; then
    # nothing cleared the "trying snap" so the boot failed
    # we clear the mode and boot normally
    set kernel_status=""
    save_env kernel_status
elif [ -n "$kernel_status" ]; then
    # ERROR invalid kernel_status state, reset to empty
    echo "invalid kernel_status!!!"
    echo "resetting to empty"
    set kernel_status=""
    save_env kernel_status
fi

if [ -e $prefix/$kernel ]; then
menuentry "Run Ubuntu Core 20" {
    # use $prefix because the symlink manipulation at runtime for kernel snap
    # upgrades, etc. should only need the /boot/grub/ directory, not the
    # /EFI/ubuntu/ directory
    chainloader $prefix/$kernel snapd_recovery_mode=run $snapd_static_cmdline_args $snapd_extra_cmdline_args
}
else
    # nothing to boot :-/
    echo "missing kernel at $prefix/$kernel!"
fi
`

const grubRecoveryConfig = `# Snapd-Boot-Config-Edition: 1

set default=0
set timeout=3
set timeout_style=hidden

if [ -e /EFI/ubuntu/grubenv ]; then
   load_env --file /EFI/ubuntu/grubenv snapd_recovery_mode snapd_recovery_system
fi

# standard cmdline params
set snapd_static_cmdline_args='console=ttyS0 console=tty1 panic=-1'

# if no default boot mode set, pick one
if [ -z "$snapd_recovery_mode" ]; then
    set snapd_recovery_mode=install
fi

if [ "$snapd_recovery_mode" = "run" ]; then
    default="run"
elif [ -n "$snapd_recovery_system" ]; then
    default=$snapd_recovery_mode-$snapd_recovery_system
fi

search --no-floppy --set=boot_fs --label ubuntu-boot

if [ -n "$boot_fs" ]; then
    menuentry "Continue to run mode" --hotkey=n --id=run {
        chainloader ($boot_fs)/EFI/boot/grubx64.efi
    }
fi

# globbing in grub does not sort
for label in /systems/*; do
    regexp --set 1:label "/([0-9]*)\$" "$label"
    if [ -z "$label" ]; then
        continue
    fi
    # yes, you need to backslash that less-than
    if [ -z "$best" -o "$label" \< "$best" ]; then
        set best="$label"
    fi
    # if grubenv did not pick mode-system, use best one
    if [ -z "$snapd_recovery_system" ]; then
        default=$snapd_recovery_mode-$best
    fi
    set snapd_recovery_kernel=
    load_env --file /systems/$label/grubenv snapd_recovery_kernel snapd_extra_cmdline_args

    # We could "source /systems/$snapd_recovery_system/grub.cfg" here as well
    menuentry "Recover using $label" --hotkey=r --id=recover-$label $snapd_recovery_kernel recover $label {
        loopback loop $2
        chainloader (loop)/kernel.efi snapd_recovery_mode=$3 snapd_recovery_system=$4 $snapd_static_cmdline_args $snapd_extra_cmdline_args
    }
    menuentry "Install using $label" --hotkey=i --id=install-$label $snapd_recovery_kernel install $label {
        loopback loop $2
        chainloader (loop)/kernel.efi snapd_recovery_mode=$3 snapd_recovery_system=$4 $snapd_static_cmdline_args $snapd_extra_cmdline_args
    }
done

menuentry 'System setup' --hotkey=f 'uefi-firmware' {
    fwsetup
}
`

func init() {
	registerInternal("grub.cfg", []byte(grubBootConfig))
	registerSnippetForEditions("grub.cfg:static-cmdline", []ForEditions{
		{FirstEdition: 1, Snippet: []byte("console=ttyS0 console=tty1 panic=-1")},
	})

	registerInternal("grub-recovery.cfg", []byte(grubRecoveryConfig))
	registerSnippetForEditions("grub-recovery.cfg:static-cmdline", []ForEditions{
		{FirstEdition: 1, Snippet: []byte("console=ttyS0 console=tty1 panic=-1")},
	})
}
