#!/usr/bin/env bash

# Copyright 2019 Antrea Authors
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

# This is a very simple script that builds the Open vSwitch base image for Antrea and pushes it to
# the Antrea Dockerhub (https://hub.docker.com/u/antrea). The image is tagged with the OVS version.

set -eo pipefail

function echoerr {
    >&2 echo "$@"
}

_usage="Usage: $0 [--pull] [--push] [--platform <PLATFORM>] [--distro [ubuntu|ubi]]
Build the antrea openvswitch image.
        --pull                      Always attempt to pull a newer version of the base images
        --push                      Push the built image to the registry
        --platform <PLATFORM>       Target platform for the image if server is multi-platform capable
        --distro <distro>           Target Linux distribution
        --download-ovs              Download OVS source code tarball from internet. Default is false.
        --ipsec                     Build with IPsec support. Default is false.
        --rpm-repo-url <url>        URL of the RPM repository to use for Photon builds."

function print_usage {
    echoerr "$_usage"
}

PULL=false
PUSH=false
IPSEC=false
PLATFORM=""
DISTRO="ubuntu"
DOWNLOAD_OVS=false
RPM_REPO_URL=""
SUPPORT_DISTROS=("ubuntu" "ubi" "debian" "photon")

while [[ $# -gt 0 ]]
do
key="$1"

case $key in
    --push)
    PUSH=true
    shift
    ;;
    --pull)
    PULL=true
    shift
    ;;
    --platform)
    PLATFORM="$2"
    shift 2
    ;;
    --distro)
    DISTRO="$2"
    shift 2
    ;;
    --download-ovs)
    DOWNLOAD_OVS=true
    shift
    ;;
    --ipsec)
    IPSEC=true
    shift
    ;;
    --rpm-repo-url)
    RPM_REPO_URL="$2"
    shift 2
    ;;
    -h|--help)
    print_usage
    exit 0
    ;;
    *)    # unknown option
    echoerr "Unknown option $1"
    exit 1
    ;;
esac
done

if [ "$PLATFORM" != "" ] && $PUSH; then
    echoerr "Cannot use --platform with --push"
    exit 1
fi

PLATFORM_ARG=""
if [ "$PLATFORM" != "" ]; then
    PLATFORM_ARG="--platform $PLATFORM"
fi

DISTRO_VALID=false
for dist in "${SUPPORT_DISTROS[@]}"; do
    if [ "$DISTRO" == "$dist" ]; then
        DISTRO_VALID=true
        break
    fi
done

if ! $DISTRO_VALID; then
    echoerr "Invalid distribution $DISTRO"
    exit 1
fi

THIS_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

pushd $THIS_DIR > /dev/null

OVS_VERSION=$(head -n 1 ../deps/ovs-version)

BUILD_TAG=$(../build-tag.sh)
if [ "$IPSEC" == "true" ]; then
    BUILD_TAG="${BUILD_TAG}-ipsec"
fi

if $DOWNLOAD_OVS; then
    curl -LO https://www.openvswitch.org/releases/openvswitch-$OVS_VERSION.tar.gz
elif [ ! -f openvswitch-$OVS_VERSION.tar.gz ]; then
    echoerr "openvswitch-$OVS_VERSION.tar.gz not found. Use --download-ovs true to download it."
    exit 1
fi

# This is a bit complicated but we make sure that we only build OVS if
# necessary, and at the moment --cache-from does not play nicely with multistage
# builds: we need to push the intermediate image to the registry. Note that the
# --cache-from option will have no effect if the image doesn't exist
# locally.
# See https://github.com/moby/moby/issues/34715.

if $PULL; then
    if [[ ${DOCKER_REGISTRY} == "" ]]; then
        docker pull $PLATFORM_ARG ubuntu:22.04
    else
        docker pull ${DOCKER_REGISTRY}/antrea/ubuntu:22.04
        docker tag ${DOCKER_REGISTRY}/antrea/ubuntu:22.04 ubuntu:22.04
    fi
    if [ "$DISTRO" == "ubuntu" ]; then
        IMAGES_LIST=(
            "antrea/openvswitch-debs:$BUILD_TAG"
            "antrea/openvswitch:$BUILD_TAG"
        )
    elif [ "$DISTRO" == "ubi" ]; then
        IMAGES_LIST=(
            "antrea/openvswitch-rpms:$BUILD_TAG"
            "antrea/openvswitch-ubi:$BUILD_TAG"
        )
    fi
    for image in "${IMAGES_LIST[@]}"; do
        if [[ ${DOCKER_REGISTRY} == "" ]]; then
            docker pull $PLATFORM_ARG "${image}" || true
        else
            rc=0
            docker pull "${DOCKER_REGISTRY}/${image}" || rc=$?
            if [[ $rc -eq 0 ]]; then
                docker tag "${DOCKER_REGISTRY}/${image}" "${image}"
            fi
        fi
    done
fi

if [ "$DISTRO" == "ubuntu" ]; then
    docker build $PLATFORM_ARG --target ovs-debs \
           --cache-from antrea/openvswitch-debs:$BUILD_TAG \
           -t antrea/openvswitch-debs:$BUILD_TAG \
           --build-arg OVS_VERSION=$OVS_VERSION \
           --build-arg IPSEC=$IPSEC .

    docker build $PLATFORM_ARG \
           --cache-from antrea/openvswitch-debs:$BUILD_TAG \
           --cache-from antrea/openvswitch:$BUILD_TAG \
           -t antrea/openvswitch:$BUILD_TAG \
           --build-arg OVS_VERSION=$OVS_VERSION \
           --build-arg IPSEC=$IPSEC .

elif [ "$DISTRO" == "ubi" ]; then
    docker build $PLATFORM_ARG --target ovs-rpms \
           --cache-from antrea/openvswitch-rpms:$BUILD_TAG \
           -t antrea/openvswitch-rpms:$BUILD_TAG \
           --build-arg OVS_VERSION=$OVS_VERSION \
           --build-arg IPSEC=$IPSEC \
           -f Dockerfile.ubi .

    docker build \
           --cache-from antrea/openvswitch-rpms:$BUILD_TAG \
           --cache-from antrea/openvswitch-ubi:$BUILD_TAG \
           -t antrea/openvswitch-ubi:$BUILD_TAG \
           --build-arg OVS_VERSION=$OVS_VERSION \
           --build-arg IPSEC=$IPSEC \
           -f Dockerfile.ubi .

elif [ "$DISTRO" == "debian" ];then
    docker build $PLATFORM_ARG --target ovs-debian-debs \
           --cache-from antrea/openvswitch-debian-debs:$BUILD_TAG \
           -t antrea/openvswitch-debian-debs:$BUILD_TAG \
           --build-arg OVS_VERSION=$OVS_VERSION \
           --build-arg IPSEC=$IPSEC \
           -f Dockerfile.debian .

    docker build $PLATFORM_ARG \
           --cache-from antrea/openvswitch-debian-debs:$BUILD_TAG \
           --cache-from antrea/openvswitch-debian:$BUILD_TAG \
           -t antrea/openvswitch-debian:$BUILD_TAG \
           --build-arg OVS_VERSION=$OVS_VERSION \
           --build-arg IPSEC=$IPSEC \
           -f Dockerfile.debian .

elif [ "$DISTRO" == "photon" ]; then
    if [ "$RPM_REPO_URL" == "" ]; then
        echoerr "Must specify --rpm-repo-url when building for Photon"
        exit 1
    fi
    if [ "$IPSEC" == "true" ]; then
        echoerr "IPsec is not supported for Photon"
        exit 1
    fi
    if ! [ -f "photon-rootfs.tar.gz" ]; then
        echoerr "photon-rootfs.tar.gz not found."
        exit 1
    fi
    docker build $PLATFORM_ARG --target ovs-photon-rpms \
           --cache-from antrea/openvswitch-photon-rpms:$BUILD_TAG \
           -t antrea/openvswitch-photon-rpms:$BUILD_TAG \
           --build-arg OVS_VERSION=$OVS_VERSION \
           --build-arg RPM_REPO_URL=$RPM_REPO_URL \
           -f Dockerfile.photon .

    docker build $PLATFORM_ARG \
           --cache-from antrea/openvswitch-photon-rpms:$BUILD_TAG \
           --cache-from antrea/openvswitch-photon:$BUILD_TAG \
           -t antrea/openvswitch-photon:$BUILD_TAG \
           --build-arg OVS_VERSION=$OVS_VERSION \
           --build-arg RPM_REPO_URL=$RPM_REPO_URL \
           -f Dockerfile.photon .

elif [ "$DISTRO" == "ubi" ]; then
    docker build $PLATFORM_ARG --target ovs-rpms \
           --cache-from antrea/openvswitch-rpms:$BUILD_TAG \
           -t antrea/openvswitch-rpms:$BUILD_TAG \
           --build-arg OVS_VERSION=$OVS_VERSION \
           --build-arg IPSEC=$IPSEC \
           -f Dockerfile.ubi .

    docker build $PLATFORM_ARG \
           --cache-from antrea/openvswitch-rpms:$BUILD_TAG \
           --cache-from antrea/openvswitch-ubi:$BUILD_TAG \
           -t antrea/openvswitch-ubi:$BUILD_TAG \
           --build-arg OVS_VERSION=$OVS_VERSION \
           --build-arg IPSEC=$IPSEC \
           -f Dockerfile.ubi .
fi

if $PUSH; then
    if [ "$DISTRO" == "ubuntu" ]; then
        docker push antrea/openvswitch-debs:$BUILD_TAG
        docker push antrea/openvswitch:$BUILD_TAG
    elif [ "$DISTRO" == "ubi" ]; then
        docker push antrea/openvswitch-rpms:$BUILD_TAG
        docker push antrea/openvswitch-ubi:$BUILD_TAG
    fi
fi

popd > /dev/null
