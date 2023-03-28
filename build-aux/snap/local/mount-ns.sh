#!/bin/bash

set -x

set -eu

# FIXME: This should be replace by a mount of devtmpfs when
# it is supported in user namespaces
bind_dev() {
    dev="${1}/dev"

    for device in null zero full random urandom tty; do
        touch "${dev}/${device}"
        mount --bind "/dev/${device}" "${dev}/${device}"
    done
}

# Because systemd debian package post-install script is not very
# robust, we have to create a symlink and bind mount resolv.conf
# there.
bind_resolv() {
    sysroot="${1}"

    resolv_real_path="${sysroot}/run/fake-resolv.conf"
    create_symlink=yes
    if [ -L "${sysroot}/etc/resolv.conf" ]; then
        resolv="$(readlink "${sysroot}/etc/resolv.conf")"
        if [ "${resolv}" = "../run/systemd/resolve/stub-resolv.conf" ]; then
            resolv_real_path="${sysroot}/run/systemd/resolve/stub-resolv.conf"
            create_symlink=no
        fi
    fi
    mkdir -p "$(dirname "${resolv_real_path}")"
    touch "${resolv_real_path}"
    mount --bind -o ro /etc/resolv.conf "${resolv_real_path}"
    if [ "${create_symlink}" = yes ]; then
        ln -srf "${resolv_real_path}" "${sysroot}/etc/resolv.conf"
    fi
}

if [ $# -lt 3 ]; then
    echo "Expected at least 3 arguments" 1>&2
    exit 1
fi

command="${1}"
sysroot="${2}"
shift 2

case "${command}" in
    spawn)
        # This is the first phase. This will re-spawn the script with
        # `init` subcommand.  There are limitations of what can be
        # done within a LXD container.  So in the first phase we mount
        # a tmpfs filesytem which cannot always be done in a mount
        # namespace. Because it is outside of the mount namespace,
        # it has to be removed from manually.
        # Then we spawn the second phase into a namespace,
        # but without changing the root.

        tmpdir="$(mktemp -d --tmpdir mount-ns.XXXXXXXXXX)"
        cleanup() {
            umount "${tmpdir}" || true
            rm -rf "${tmpdir}"
        }
        mount -t tmpfs tmpfs "${tmpdir}"
        mkdir -m 0755 "${tmpdir}/dev"
        mkdir -m 1777 "${tmpdir}/tmp"
        mkdir -m 0755 "${tmpdir}/run"
        case "${sysroot}" in
            /)
              options=()
              ;;
            *)
              options=(
                  --bind "${tmpdir}/dev" /dev
                  --bind "${tmpdir}/tmp" /tmp
                  --bind "${tmpdir}/run" /run
              )
              ;;
        esac
        trap cleanup EXIT
        unshare --pid --fork --mount -- "${0}" init "${sysroot}" "${options[@]}" "${@}"
        ;;
    init)
        # This is the second phase. Here we are in a mount namespace,
        # spawned from the `spawn` subcommand.  But we still have the
        # same root directory. So we can bind mount all we need in the
        # sysroot. Then we can change the root to that sysroot.

        mount -t proc proc "${sysroot}/proc"
        while [ $# -gt 1 ]; do
            case "${1}" in
                --)
                    shift
                    break
                    ;;
                --bind|--ro-bind)
                    if [ -d "$2" ]; then
                        if ! [ -d "${sysroot}/$3" ]; then
                            mkdir -p "${sysroot}/$3"
                        fi
                    else
                        if ! [ -e "${sysroot}/$3" ]; then
                            dir="$(dirname "${sysroot}/$3")"
                            if ! [ -d "${dir}" ]; then
                                mkdir -p "${dir}"
                            fi
                            touch "${sysroot}/$3"
                        fi
                    fi
                    extra_args=()
                    case "$1" in
                        --ro-bind)
                            extra_args=("-o" "ro")
                            ;;
                    esac
                    mount --bind "${extra_args[@]}" "$2" "${sysroot}/$3"
                    shift 3
                    ;;
                *)
                    break
                    ;;
            esac
        done
        bind_dev "${sysroot}"
        bind_resolv "${sysroot}"
        case "${sysroot}" in
            /)
              options=()
              ;;
            *)
              options=(
                  --root="${sysroot}"
              )
        esac
        exec unshare --mount "${options[@]}" -- "${@}"
        ;;
    *)
        echo "Unknown command" 1>&2
        exit 1
        ;;
esac
