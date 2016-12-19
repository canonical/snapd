#!bash
bootenv() {
    if [ $# -eq 0 ]; then
        if command -v grub-editenv >/dev/null; then
            grub-editenv list
        else
            fw_printenv
        fi
    else
        if command -v grub-editenv >/dev/null; then
            grub-editenv list | grep "^$1"
        else
            fw_printenv "$1"
        fi | sed "s/^${1}=//"
    fi
}

bootenv_set() {
    local name=$1
    local value=$2

    if command -v grub-editenv >/dev/null; then
        if [ -z "$value" ]; then
            grub-editenv unset "$name"
        else
            grub-editenv set "$name" "$value"
        fi
    else
        fw_setenv "$name" "$value"
    fi
}
