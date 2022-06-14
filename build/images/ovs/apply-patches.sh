#!/usr/bin/env bash

# Copyright 2020 Antrea Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script applies unreleased patches (or released in a more recent version
# of OVS than the one Antrea is using) to OVS before building it. It needs to be
# run from the root of the OVS source tree.

set -eo pipefail

function echoerr {
    >&2 echo "$@"
}

# Inspired from https://stackoverflow.com/a/24067243/4538702
# 'sort -V' is available on Ubuntu 20.04
# less than
function version_lt() { test "$(printf '%s\n' "$@" | sort -rV | head -n 1)" != "$1"; }
# greater than
function version_gt() { test "$(printf '%s\n' "$@" | sort -V | head -n 1)" != "$1"; }
# less than or equal to
function version_let() { test "$(printf '%s\n' "$@" | sort -V | head -n 1)" == "$1"; }
# greater than or equal to
function version_get() { test "$(printf '%s\n' "$@" | sort -rV | head -n 1)" == "$1"; }

function apply_patch() {
    commit_sha="$1"
    shift
    curl -s "https://github.com/openvswitch/ovs/commit/$commit_sha.patch" | \
        git apply "$@"
}

# OVS hardcodes the installation path to /usr/lib/python3.7/dist-packages/ but this location
# does not seem to be in the Python path in Ubuntu 22.04. There may be a better way to do this,
# but this seems like an acceptable workaround.
sed -i 's/python3\.7/python3\.10/' debian/openvswitch-test.install
sed -i 's/python3\.7/python3\.10/' debian/python3-openvswitch.install
