#!/bin/bash

clean_snapd_lib() {
    rm -rf /var/lib/snapd/assertions/*
    rm -rf /var/lib/snapd/device
    rm -rf /var/lib/snapd/state.json
}

prepare_core_model(){
    mv /var/lib/snapd/seed/assertions/model model.bak
}

restore_core_model(){
    mv model.bak /var/lib/snapd/seed/assertions/model
}

prepare_and_manip_seed(){
    cp /var/lib/snapd/seed/seed.yaml seed.yaml.bak
    python3 "$TESTSLIB"/manip_seed.py /var/lib/snapd/seed/seed.yaml "$@"
}

restore_updated_seed(){
    mv seed.yaml.bak /var/lib/snapd/seed/seed.yaml
}

prepare_testrootorg_store(){
    cp -f "$TESTSLIB"/assertions/testrootorg-store.account-key /var/lib/snapd/seed/assertions
}

restore_testrootorg_store(){
    rm -f /var/lib/snapd/seed/assertions/testrootorg-store.account-key
}

prepare_test_account(){
    local ACCOUNT_NAME=$1
    cp -f "$TESTSLIB/assertions/${ACCOUNT_NAME}.account" /var/lib/snapd/seed/assertions
    cp -f "$TESTSLIB/assertions/${ACCOUNT_NAME}.account-key" /var/lib/snapd/seed/assertions
}

restore_test_account(){
    local ACCOUNT_NAME=$1
    rm -f "/var/lib/snapd/seed/assertions/${ACCOUNT_NAME}.account"
    rm -f "/var/lib/snapd/seed/assertions/${ACCOUNT_NAME}.account-key"
}

prepare_test_model(){
    local MODEL_NAME=$1
    local MODEL_FINAL
    MODEL_FINAL=$(get_test_model "$MODEL_NAME")
    cp -f "${TESTSLIB}/assertions/${MODEL_FINAL}" "/var/lib/snapd/seed/assertions/${MODEL_NAME}.model"
}

restore_test_model(){
    local MODEL_NAME=$1
    rm -f "/var/lib/snapd/seed/assertions/${MODEL_NAME}.model"
}

unpack_pc_snap(){
    unsquashfs -no-progress /var/lib/snapd/snaps/pc_*.snap
}

pack_pc_snap(){
    mksquashfs squashfs-root pc_x1.snap -comp xz -no-fragments -no-progress
    rm -rf squashfs-root
    cp pc_x1.snap /var/lib/snapd/seed/snaps/
}

restore_pc_snap(){
    if systemctl status snap-pc-x1.mount ; then
       systemctl stop snap-pc-x1.mount
       rm -f /etc/systemd/system/snap-pc-x1.mount
       rm -f /etc/systemd/system/snapd.mounts.target.wants/snap-pc-x1.mount
       rm -f /etc/systemd/system/multi-user.target.wants/snap-pc-x1.mount
       rm -f /var/lib/snapd/snaps/pc_x1.snap
       rm -rf /root/snap/pc/x1
       systemctl daemon-reload
    fi
    rm -f /var/lib/snapd/seed/snaps/pc_x1.snap
}

get_test_model(){
    local MODEL_NAME=$1
    if os.query is-core18; then
        echo "${MODEL_NAME}-18.model"
    elif os.query is-core20; then
        echo "${MODEL_NAME}-20.model"
    elif os.query is-core22; then
        echo "${MODEL_NAME}-22.model"
    else
        echo "${MODEL_NAME}.model"
    fi
}

get_test_snap_suffix(){
    if os.query is-core18; then
        echo "-core18"
    elif os.query is-core20; then
        echo "-core20"
    elif os.query is-core22; then
        echo "-core22"
    fi
}

wait_for_first_boot_change(){
    retry -n 200 --wait 1 sh -c 'snap changes | MATCH "Done.*Initialize system state"'
}

wait_for_device_initialized_change(){
    retry -n 200 --wait 1 sh -c 'snap changes | MATCH "Done.*Initialize device"'
}
