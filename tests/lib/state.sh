#!/bin/sh
save_state(){
    cp /var/lib/snapd/state.json $SPREAD_PATH/state.json.save
}
remove_users_from_state(){
    snap install jq-cprov
    systemctl stop snapd.service
    cp /var/lib/snapd/state.json /var/snap/jq-cprov/state.json~
    jq-cprov.jq 'del(.data.auth.users)|.' /var/snap/jq-cprov/state.json~ > /var/snap/jq-cprov/state.json
    cp /var/snap/jq-cprov/state.json /var/lib/snapd/state.json
    rm /var/snap/jq-cprov/state.json~
    systemctl start snapd.service
    snap remove jq-cprov
}

restore_state(){
    if [ -f $SPREAD_PATH/state.json.save ]; then
        systemctl stop snapd.service
        mv $SPREAD_PATH/state.json.save /var/lib/snapd/state.json
        systemctl start snapd.service
    fi
}
