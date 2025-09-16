<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: Zygmunt Krynicki
-->
# plz-run!

This project, pronounced "please run", aims to enable sandboxed applications to
run new programs across a sandbox boundary. Given the permissions required to
reach out, across the D-Bus system bus, to systemd, a confined process may run
an unconfined payload, perhaps another confined program using different
confinement profile.
