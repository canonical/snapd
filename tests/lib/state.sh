#!/bin/sh
remove_users_from_state(){
    systemctl stop snapd.service
    cp /var/lib/snapd/state.json ./state.json.back
    sed -i 's/"users":\[[^]]*\]/"users":null/' /var/lib/snapd/state.json
    systemctl start snapd.service
}

restore_state(){
    if [ -f ./state.json.back ]; then
        systemctl stop snapd.service
        mv ./state.json.back /var/lib/snapd/state.json
        systemctl start snapd.service
    fi
}
