#!/bin/sh
remove_users_from_state(){
    cp /var/lib/snapd/state.json ./state.json.back
    sed -i 's/"users":\[[^]]*\]/"users":null/' /var/lib/snapd/state.json
}

restore_state(){
    if [ -f ./state.json.back ]; then
        mv ./state.json.back /var/lib/snapd/state.json
    fi
}
