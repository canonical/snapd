#!/bin/sh
# Build additional utilities we need for testing

on_prepare_project() {
    go get ./tests/lib/fakedevicesvc
    go get ./tests/lib/systemd-escape
}
