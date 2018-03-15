#!/bin/bash

wait_for_file() {
    the_file="$1"
    iters="$2"
    sleep_time="$3"

    for _ in $(seq "$iters"); do
        if [ -f "$the_file" ]; then
            return 0
        fi
        sleep "$sleep_time"
    done
    return 1
}

ensure_dir_exists() {
    dir="$1"
    if ! [ -d "$dir" ]; then
        mkdir -p "$dir"
        touch "$dir.fake"
    fi
}

clean_dir() {
    dir="$1"
    if [ -f "$dir.fake" ]; then
        rm -rf "$dir"
        rm -f "$dir.fake"
    fi
}

ensure_file_exists() {
    file="$1"
    if ! [ -f "$file" ]; then
        touch "$file"
        touch "$file.fake"
    fi
}

ensure_file_exists_backup_real() {
    file="$1"
    if [ -f "$file" ]; then
        mv "$file" "$file.back"
    fi
    touch "$file"
    touch "$file.fake"
}

clean_file() {
    file="$1"
    if [ -f "$file.fake" ]; then
        rm -f "$file"
        rm -f "$file.fake"
    fi
    if [ -f "$file.back" ]; then
        mv "$file.back" "$file"
    fi
}
