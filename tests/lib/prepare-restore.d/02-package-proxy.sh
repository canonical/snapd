#!/bin/sh
# To accelerate testing in qemu, which is typically used on developer
# workstations without dedicated network link to the backbone of the net, allow
# configuring a proxy for the native package manager.

on_prepare_project() {
    if [ "$SPREAD_BACKEND" != qemu ]; then
        return
    fi
    if [ -d /etc/apt/apt.conf.d ]; then
        # qemu images may be built with pre-baked proxy settings that can be wrong
        rm -f /etc/apt/apt.conf.d/90cloud-init-aptproxy
        rm -f /etc/apt/apt.conf.d/99proxy
        if [ -n "${HTTP_PROXY:-}" ]; then
            printf 'Acquire::http::Proxy "%s";\n' "$HTTP_PROXY" >> /etc/apt/apt.conf.d/99proxy
        fi
        if [ -n "${HTTPS_PROXY:-}" ]; then
            printf 'Acquire::https::Proxy "%s";\n' "$HTTPS_PROXY" >> /etc/apt/apt.conf.d/99proxy
        fi
    fi
    if [ -f /etc/dnf/dnf.conf ]; then
        if [ -n "${HTTP_PROXY:-}" ]; then
            echo "proxy=$HTTP_PROXY" >> /etc/dnf/dnf.conf
        fi
    fi
    # TODO: zypper proxy, yum proxy
}
