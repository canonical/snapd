#!/bin/sh
remove_users_from_state(){
    snap install jq-cprov.jq
    systemctl stop snapd.service
    cp /var/lib/snapd/state.json ./state.json.back
    mv /var/lib/snapd/state.json /var/lib/snapd/state.json~
    jq-cprov.jq 'del(.data.auth.users)|.' /var/lib/snapd/state.json~ > /var/lib/snapd/state.json
    rm /var/lib/snapd/state.json~
    systemctl start snapd.service
    snap remove jq-cprov
}

restore_state(){
    if [ -f ./state.json.back ]; then
        systemctl stop snapd.service
        mv ./state.json.back /var/lib/snapd/state.json
        systemctl start snapd.service
    fi
}
