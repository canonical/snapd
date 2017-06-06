#!/bin/bash

# shellcheck source=tests/lib/pkgdb.sh
. "$TESTSLIB"/pkgdb.sh

curr_history=/var/log/apt/history.log
full_history=/var/log/apt/history.log.bak

install_build_snapd(){
    if [ "$SRU_VALIDATION" = "1" ]; then
        apt install -y snapd
        cp /etc/apt/sources.list sources.list.back
        echo "deb http://archive.ubuntu.com/ubuntu/ $(lsb_release -c -s)-proposed restricted main multiverse universe" | tee /etc/apt/sources.list -a
        apt update
        apt install -y --only-upgrade snapd
        mv sources.list.back /etc/apt/sources.list
        apt update
    else
        distro_install_local_package "$GOHOME"/snapd_*.deb
    fi
}

clean_apt_history(){
    if [ -f "$curr_history" ]; then
        mv "$curr_history" "$full_history"
    fi
}

remove_installed_apt_packages(){
    if [ -f "$curr_history" ]; then
        packages=$(grep -e "^Install:" "$curr_history" | awk '{gsub( /\([^()]*\)/ ,"" );gsub(/ ,/," ");sub(/^Install:/,""); print}' | tr '\n' ' ')
        apt-get remove -y --purge $packages || echo "Failed removing packages: $packages"
    fi
}

restore_apt_history(){
    if [ -f "$full_history" ]; then
        if [ -f "$curr_history" ]; then
            cat "$curr_history" >> "$full_history"
        fi
        mv "$full_history" "$curr_history"
    fi
}
