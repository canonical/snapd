#!/bin/bash

# Guest setup helpers for SSH users, proxy and NTP configuration, and test tools.

nested_prepare_ssh() {
    if nested_is_core_ge 24; then
        remote.exec "sudo useradd --uid 12345 --create-home --extrausers test"
        remote.exec "sudo useradd --create-home --extrausers external"
    else 
        remote.exec "sudo adduser --uid 12345 --extrausers --quiet --disabled-password --gecos '' test"
        remote.exec "sudo adduser --extrausers --quiet --disabled-password --gecos '' external"
    fi
    
    remote.exec "echo test:ubuntu123 | sudo chpasswd"
    remote.exec "echo 'test ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/create-user-test"
    # Check we can connect with the new test user and make sudo
    remote.exec --user test --pass ubuntu123 "sudo true"

    remote.exec "echo external:ubuntu123 | sudo chpasswd"
    remote.exec "echo 'external ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/create-user-external"
    # Check we can connect with the new external user and make sudo
    remote.exec --user external --pass ubuntu123 "sudo true"
}


nested_setup_vm(){
    echo "Setting up the nested VM"
    local modified
    modified=0

    if [ "${SNAPD_USE_PROXY:-}" = true ]; then
        echo "Configuring proxy on the nested VM"
        nested_no_proxy="${NO_PROXY},10.0.2.2"

        # Ensure the nameservers used are the same than the host vm
        if os.query is-ubuntu-ge 18.04; then
            net_interface="$(ip route show default | awk '{print $5}')"
            nameservers="$(resolvectl status "$net_interface" | grep "DNS Servers:" | cut -d: -f2)"
            if [ -n "$nameservers" ]; then
                remote.exec "grep -v '^nameserver' /etc/resolv.conf > /tmp/resolv.conf"
                for nameserver in $nameservers; do
                    remote.exec "echo nameserver $nameserver >> /tmp/resolv.conf"
                done
                remote.exec "sudo cp /tmp/resolv.conf /etc/resolv.conf"
            fi
        else
            remote.push /etc/resolv.conf
            remote.exec "sudo cp resolv.conf /etc/resolv.conf"
            remote.exec "rm resolv.conf"
        fi

        # Add proxy configuration in /etc/environment
        remote.exec "echo HTTPS_PROXY=$HTTPS_PROXY | sudo tee -a /etc/environment"
        remote.exec "echo https_proxy=$HTTPS_PROXY | sudo tee -a /etc/environment"
        remote.exec "echo HTTP_PROXY=$HTTP_PROXY | sudo tee -a /etc/environment"
        remote.exec "echo http_proxy=$HTTP_PROXY | sudo tee -a /etc/environment"
        remote.exec "echo NO_PROXY=$nested_no_proxy | sudo tee -a /etc/environment"
        remote.exec "echo no_proxy=$nested_no_proxy | sudo tee -a /etc/environment"
        remote.exec "echo SNAPD_USE_PROXY=$SNAPD_USE_PROXY | sudo tee -a /etc/environment"

        # Configure snapd to use the proxy
        remote.retry -n 10 --wait 3 "systemctl is-enabled snapd"
        remote.exec "sudo systemctl stop snapd.service snapd.socket"
        remote.exec "sudo mkdir -p /etc/systemd/system/snapd.service.d"
        remote.exec "echo [Service] | sudo tee /etc/systemd/system/snapd.service.d/proxy.conf"
        remote.exec "echo Environment=HTTPS_PROXY=$HTTPS_PROXY HTTP_PROXY=$HTTP_PROXY https_proxy=$HTTPS_PROXY http_proxy=$HTTP_PROXY NO_PROXY=$nested_no_proxy no_proxy=$nested_no_proxy SNAPD_USE_PROXY=$SNAPD_USE_PROXY | sudo tee -a /etc/systemd/system/snapd.service.d/proxy.conf"
        remote.exec "sudo systemctl daemon-reload"
        remote.exec "sudo systemctl start snapd.service snapd.socket"
        modified=1
        echo "Proxy configuration added to the nested VM"
    fi

    if [ -n "${NTP_SERVER:-}" ]; then
        echo "Configuring NTP server on the nested VM"
        # We reconfigure both chrony and timesyncd if installed. But
        # we only restart the one started.
        if remote.exec "[ -d /etc/chrony/sources.d ]"; then
            remote.exec "sudo rm /etc/chrony/sources.d/*.sources"
            echo "pool ${NTP_SERVER} iburst maxsources 1 nts prefer" | remote.exec "sudo tee /etc/chrony/sources.d/proxy.sources"
            # try-restart will not restart if not started. Important
            # if both timesyncd and chrony are installed but only one is
            # running
            remote.exec "sudo systemctl try-restart chrony.service"
            modified=1
        fi
        if remote.exec "[ -f /etc/systemd/timesyncd.conf ]"; then
            # Configure systemd-timesyncd to use the predefined ntp server
            CONF_FILE="/etc/systemd/timesyncd.conf"
            remote.exec "cp \"$CONF_FILE\" /tmp/timesyncd.conf"
            remote.exec "sed -i -e '/^NTP=/d' -e '/^FallbackNTP=/d' /tmp/timesyncd.conf"
            remote.exec "sed -i '/^\[Time\]/a NTP='\"$NTP_SERVER\" /tmp/timesyncd.conf"
            remote.exec "sed -i '/^\[Time\]/a FallbackNTP=' /tmp/timesyncd.conf"
            remote.exec "sudo cp /tmp/timesyncd.conf \"$CONF_FILE\""
            # try-restart will not restart if not started. Important
            # if both timesyncd and chrony are installed but only one is
            # running
            remote.exec "sudo systemctl try-restart systemd-timesyncd"
            modified=1
        fi
        echo "NTP server configuration added to the nested VM"
    fi

    if [ "${modified}" != 0 ]; then
      echo "Modifications have been made to the nested VM, syncing changes to disk"
      # Some modification have happened, before return back to a test
      # that might do a hard reset, we need to make sure the
      # modification are saved to disk.
      remote.exec "sudo sync"
    fi
}

remote.exec_as() {
    local USER="$1"
    local PASSWD="$2"
    shift 2
    sshpass -p "$PASSWD" ssh -p "$NESTED_SSH_PORT" -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no "$USER"@localhost "$@"
}

nested_prepare_tools() {
    echo "Preparing test tools in nested vm"

    TOOLS_PATH=/writable/test-tools
    if ! remote.exec "test -d $TOOLS_PATH" &>/dev/null; then
        remote.exec "sudo mkdir -p $TOOLS_PATH"
        remote.exec "sudo chown $NESTED_REMOTE_USER_NAME:$NESTED_REMOTE_USER_NAME $TOOLS_PATH"
    fi

    if ! remote.exec "test -e $TOOLS_PATH/retry" &>/dev/null; then
        remote.push "$TESTSTOOLS/retry"
        remote.exec "mv retry $TOOLS_PATH/retry"
        echo "retry tool copied to nested vm"
    fi

    if ! remote.exec "test -e $TOOLS_PATH/not" &>/dev/null; then
        remote.push "$TESTSTOOLS/not"
        remote.exec "mv not $TOOLS_PATH/not"
        echo "not tool copied to nested vm"
    fi

    if ! remote.exec "test -e $TOOLS_PATH/MATCH" &>/dev/null; then
        # shellcheck source=tests/lib/spread-funcs.sh
        . "$TESTSLIB"/spread-funcs.sh
        echo '#!/bin/bash' > MATCH_FILE
        type MATCH | tail -n +2 >> MATCH_FILE
        echo 'MATCH "$@"' >> MATCH_FILE
        chmod +x MATCH_FILE
        remote.push "MATCH_FILE"
        remote.exec "mv MATCH_FILE $TOOLS_PATH/MATCH"
        rm -f MATCH_FILE
        echo "MATCH tool copied to nested vm"
    fi

    if ! remote.exec "test -e $TOOLS_PATH/NOMATCH" &>/dev/null; then
        # shellcheck source=tests/lib/spread-funcs.sh
        . "$TESTSLIB"/spread-funcs.sh
        echo '#!/bin/bash' > NOMATCH_FILE
        type NOMATCH | tail -n +2 >> NOMATCH_FILE
        echo 'NOMATCH "$@"' >> NOMATCH_FILE
        chmod +x NOMATCH_FILE
        remote.push "NOMATCH_FILE"
        remote.exec "mv NOMATCH_FILE $TOOLS_PATH/NOMATCH"
        rm -f NOMATCH_FILE
        echo "NOMATCH tool copied to nested vm"
    fi

    if ! remote.exec "grep -qE PATH=.*$TOOLS_PATH /etc/environment"; then
        # shellcheck disable=SC2016
        REMOTE_PATH="$(remote.exec 'echo $PATH')"
        remote.exec "echo PATH=$TOOLS_PATH:$REMOTE_PATH:/usr/lib/python | sudo tee -a /etc/environment"
        echo "PATH updated in /etc/environment"
    fi

    if [ -n "$TAG_FEATURES" ]; then
        # To cover also tests that don't repack the gadget snap, add feature tagging using env variable drop-ins
        remote.exec "printf 'SNAPD_DEBUG=1\nSNAPPY_TESTING=1\nSNAPD_TRACE=1\nSNAPD_JSON_LOGGING=1\nSNAP_LOG_TO_JOURNAL=1\n' | sudo tee -a /etc/environment"
        CONF_FILE=99-generate-coverage.conf
        while IFS= read -r line; do
            dir=$(sed -E 's|^(.*)\.in$|/etc/systemd/system/\1.d|' <<<"$line")
            remote.exec "sudo mkdir -p $dir"
            remote.exec "printf '[Service]\nEnvironment=SNAPD_DEBUG=1\nEnvironment=SNAPPY_TESTING=1\nEnvironment=SNAPD_TRACE=1\nEnvironment=SNAPD_JSON_LOGGING=1\n' | sudo tee $dir/$CONF_FILE"    
        done < <(find "$SPREAD_PATH"/data/systemd "$SPREAD_PATH"/data/systemd-user -type f -name '*.service.in' -exec basename {} \;)
        remote.exec "sudo systemctl daemon-reload"
        remote.exec "sudo systemctl restart snapd"
        remote.exec "sudo snap set system journal.persistent=true"
    fi
}

nested_fetch_spread() {
    mkdir -p "$NESTED_WORK_DIR"
    rm -f "$NESTED_WORK_DIR/spread"

    if os.query is-arm; then
        curl -s https://storage.googleapis.com/snapd-spread-tests/spread/spread-plus-arm64.tar.gz | tar -xz -C "$NESTED_WORK_DIR"
        echo "$NESTED_WORK_DIR/spread"
        return 
    fi

    if [ ! -f /snap/bin/spread-plus.spread ]; then
        snap install spread-plus --beta --devmode &>/dev/null
    fi
    echo "/snap/bin/spread-plus.spread"

}

