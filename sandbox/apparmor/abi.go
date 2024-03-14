// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
package apparmor

// textABI30 is a text of the apparmor ABI at version 3.0
const textABI30 = `
query {label {multi_transaction {yes
}
data {yes
}
perms {allow deny audit quiet
}
}
}
dbus {mask {acquire send receive
}
}
signal {mask {hup int quit ill trap abrt bus fpe kill usr1 segv usr2 pipe alrm term stkflt chld cont stop stp ttin ttou urg xcpu xfsz vtalrm prof winch io pwr sys emt lost
}
}
ptrace {mask {read trace
}
}
caps {mask {chown dac_override dac_read_search fowner fsetid kill setgid setuid setpcap linux_immutable net_bind_service net_broadcast net_admin net_raw ipc_lock ipc_owner sys_module sys_rawio sys_chroot sys_ptrace sys_pacct sys_admin sys_boot sys_nice sys_resource sys_time sys_tty_config mknod lease audit_write audit_control setfcap mac_override mac_admin syslog wake_alarm block_suspend audit_read perfmon bpf
}
}
rlimit {mask {cpu fsize data stack core rss nproc nofile memlock as locks sigpending msgqueue nice rtprio rttime
}
}
capability {0xffffff
}
namespaces {pivot_root {no
}
profile {yes
}
}
mount {mask {mount umount pivot_root
}
}
network {af_unix {yes
}
af_mask {unspec unix inet ax25 ipx appletalk netrom bridge atmpvc x25 inet6 rose netbeui security key netlink packet ash econet atmsvc rds sna irda pppox wanpipe llc ib mpls can tipc bluetooth iucv rxrpc isdn phonet ieee802154 caif alg nfc vsock kcm qipcrtr smc xdp
}
}
network_v8 {af_mask {unspec unix inet ax25 ipx appletalk netrom bridge atmpvc x25 inet6 rose netbeui security key netlink packet ash econet atmsvc rds sna irda pppox wanpipe llc ib mpls can tipc bluetooth iucv rxrpc isdn phonet ieee802154 caif alg nfc vsock kcm qipcrtr smc xdp
}
}
file {mask {create read write exec append mmap_exec link lock
}
}
domain {version {1.2
}
attach_conditions {xattr {yes
}
}
computed_longest_left {yes
}
post_nnp_subset {yes
}
fix_binfmt_elf_mmap {yes
}
stack {yes
}
change_profile {yes
}
change_onexec {yes
}
change_hatv {yes
}
change_hat {yes
}
}
policy {set_load {yes
}
versions {v8 {yes
}
v7 {yes
}
v6 {yes
}
v5 {yes
}
}
}
`
