#!/bin/sh -eux

# Measurement format, increment when making changes and hacking on this script
# on a target using spread -reuse.
F=0

# Prepare per-measurement directory
M="/tmp/.measure.format-$F"

mkdir -p "$M"
N=$(test -s "$M/.n" && cat "$M/.n" || echo 0)
mkdir -p "$M/run-$N"
cd "$M/run-$N"

while [ "$#" -gt 0 ]; do
	case "$1" in
		kernel)
			# Measure: kernel version, module list, mount table, process list and sysctl settings
			command -v uname && uname -a > kernel.version
			command -v lsmod && lsmod | (
			# can be started by systemd automount unit.
			grep -E -v '^binfmt_misc ' |
				cat
			) > kernel.modules
			test -f /proc/self/mountinfo && cat /proc/self/mountinfo | (
				grep -E -v '^[[:digit::]]+ [[:digit:]]+ 0:40 / /proc/sys/fs/binfmt_misc rw,relatime shared:[[:digit:]]+ - binfmt_misc binfmt_misc rw$' |
				cat
			) > kernel.mountinfo
			command -v ps -A -o pid,comm > kernel.procs
			command -v sysctl && sysctl -a | (
				grep -E -v '^fs.dentry-state = ' |
				grep -E -v '^fs.file-nr = ' |
				grep -E -v '^fs.inode-nr = ' |
				grep -E -v '^fs.inode-state = ' |
				grep -E -v '^kernel.ns_last_pid = ' |
				grep -E -v '^kernel.random.entropy_avail = ' |
				grep -E -v '^kernel.random.uuid = ' |
				grep -E -v '^kernel.sched_domain.cpu[[:digit:]]+.domain[[:digit:]].max_newidle_lb_cost = ' |
				cat
			)> kernel.sysctl
			;;

		apparmor)
			# Measure: loaded apparmor profiles
			# NOTE: the structure of the path is very significant, including the trailing dot.
			test -d /sys/kernel/security/apparmor/policy/. && tar --mtime=/ -cf lsm.apparmor.tar /sys/kernel/security/apparmor/policy/.
			;;

		packages)
			# Measure: installed packages
			command -v dpkg && dpkg --get-selections | sort > packages.deb
			command -v rpm && rpm -qa | sort > packages.rpm
			command -v snap && snap list --all --color=never --unicode-never | sort > packages.snap
			;;

		systemd)
			# Measure: systemd units. Because spread uses ssh and ssh creates user
			# sessions filter out differences is session-NNN.scope. Due to the nature of
			# CPU scaling  the "ondemand" service may change at any time it is filtered
			# out.
			command -v systemctl && systemctl list-units --full | (
			grep -E -v '^session-[[:digit:]]+\.scope' |
				grep -E -v '^user-[[:digit:]]+\.slice' |
				grep -E -v '^ondemand.service' |
				grep -E -v '^online.service' |
				cat
			) > systemd.units
			command -v systemctl && systemctl list-jobs --full | > systemd.jobs
			command -v systemctl && systemctl list-timers --full | > systemd.timers
			;;

		files)
			# Measure: interesting directories.
			command -v tar && test -d /etc && tar --mtime=/ -cf files.etc.tar /etc/
			command -v tar && test -d /dev && tar --mtime=/ -cf files.dev.tar /dev/
			command -v tar && test -d /boot && tar --mtime=/ -cf files.boot.tar /dev/
			command -v tar && test -d /home && tar --mtime=/ -cf files.home.tar /home/
			;;
		*)
			echo "unknown measurement: $1"
			exit 1
			;;
	esac
	shift
done

# Ensure we are not deviated
if [ "$N" -gt 0 ]; then
	diff -ur "$M/run-0" "$M/run-$N"
fi

# Store counter if we didn't fail
echo "$((N + 1))" > "$M/.n"

