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

# This is a very simple script that builds the base image for Antrea and pushes it to
# the Antrea Dockerhub (https://hub.docker.com/u/antrea). The image is tagged with the OVS version.

set -eo pipefail

function echoerr {
    >&2 echo "$@"
}

_usage="Usage: $0 [--pull] [--push] [--platform <PLATFORM>] [--distro [ubuntu|ubi]]
Build the antrea base image.
        --pull                  Always attempt to pull a newer version of the base images
        --push                  Push the built image to the registry
        --platform <PLATFORM>   Target platform for the image if server is multi-platform capable
        --download-cni-binaries Download CNI binaries from internet. You can also download the
                                binaries manually and put them in the current directory.
                                Currently cni-plugins-*.tgz and whereabouts-*.tgz are required.
        --ipsec                 Build with IPsec support Default is false.
        --distro <distro>       Target Linux distribution.
        --use-public-photon     Use public Photon repository. Should only be used in CI and for local testing.
        --rpm-repo-url <url>    URL of the RPM repository to use for Photon builds."

function print_usage {
    echoerr "$_usage"
}

PULL=false
PUSH=false
PLATFORM=""
DISTRO="ubuntu"
DOWNLOAD_CNI_BINARIES=false
IPSEC=false
RPM_REPO_URL=""
USE_PUBLIC_PHOTON=false
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
    --ipsec)
    IPSEC=true
    shift
    ;;
    --download-cni-binaries)
    DOWNLOAD_CNI_BINARIES=true
    shift
    ;;
    --rpm-repo-url)
    RPM_REPO_URL="$2"
    shift 2
    ;;
    --use-public-photon)
    USE_PUBLIC_PHOTON=true
    shift
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

CNI_BINARIES_VERSION=$(head -n 1 ../deps/cni-binaries-version)
SURICATA_VERSION=$(head -n 1 ../deps/suricata-version)

BUILD_TAG=$(../build-tag.sh)
if [ "$IPSEC" == "true" ]; then
    BUILD_TAG="${BUILD_TAG}-ipsec"
fi

# Ignore the version of the CNI binaries if we do not want to download them.
if ! [ ${DOWNLOAD_CNI_BINARIES} == "true" ] && ! compgen -G "cni-plugins-*.tgz" > /dev/null; then
    echoerr "CNI binaries tarball not found. Use --download-cni-binaries to download it."
    exit 1
fi

if ! [ ${DOWNLOAD_CNI_BINARIES} == "true" ] && ! compgen -G "whereabouts-*.tgz" > /dev/null; then
    echoerr "Whereabouts tarball not found. Use --download-cni-binaries to download it."
    exit 1
fi

if $PULL; then
    if [[ ${DOCKER_REGISTRY} == "" ]]; then
        docker pull $PLATFORM_ARG ubuntu:22.04
    else
        docker pull ${DOCKER_REGISTRY}/antrea/ubuntu:22.04
        docker tag ${DOCKER_REGISTRY}/antrea/ubuntu:22.04 ubuntu:22.04
    fi

    if [ "$DISTRO" == "ubuntu" ]; then
        IMAGES_LIST=(
            "antrea/openvswitch:$BUILD_TAG"
            "antrea/cni-binaries:$CNI_BINARIES_VERSION"
            "antrea/base-ubuntu:$BUILD_TAG"
        )
    elif [ "$DISTRO" == "ubi" ]; then
        IMAGES_LIST=(
            "antrea/openvswitch-ubi:$BUILD_TAG"
            "antrea/cni-binaries:$CNI_BINARIES_VERSION"
            "antrea/base-ubi:$BUILD_TAG"
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

docker build $PLATFORM_ARG --target cni-binaries \
       --cache-from antrea/cni-binaries:$CNI_BINARIES_VERSION \
       -t antrea/cni-binaries:$CNI_BINARIES_VERSION \
       --build-arg DOWNLOAD_CNI_BINARIES=$DOWNLOAD_CNI_BINARIES \
       --build-arg CNI_BINARIES_VERSION=$CNI_BINARIES_VERSION \
       --build-arg BUILD_TAG=$BUILD_TAG .

if [ "$DISTRO" == "ubuntu" ]; then
    docker build $PLATFORM_ARG \
           --cache-from antrea/cni-binaries:$CNI_BINARIES_VERSION \
           --cache-from antrea/base-ubuntu:$BUILD_TAG \
           -t antrea/base-ubuntu:$BUILD_TAG \
           --build-arg DOWNLOAD_CNI_BINARIES=$DOWNLOAD_CNI_BINARIES \
           --build-arg CNI_BINARIES_VERSION=$CNI_BINARIES_VERSION \
           --build-arg SURICATA_VERSION=$SURICATA_VERSION \
           --build-arg BUILD_TAG=$BUILD_TAG .
elif [ "$DISTRO" == "ubi" ]; then
    docker build $PLATFORM_ARG \
           --cache-from antrea/cni-binaries:$CNI_BINARIES_VERSION \
           --cache-from antrea/base-ubi:$BUILD_TAG \
           -t antrea/base-ubi:$BUILD_TAG \
           -f Dockerfile.ubi \
           --build-arg CNI_BINARIES_VERSION=$CNI_BINARIES_VERSION \
           --build-arg SURICATA_VERSION=$SURICATA_VERSION \
           --build-arg BUILD_TAG=$BUILD_TAG .
elif [ "$DISTRO" == "debian" ]; then
    docker build $PLATFORM_ARG \
           --cache-from antrea/cni-binaries:$CNI_BINARIES_VERSION \
           --cache-from antrea/base-debian:$BUILD_TAG \
           -t antrea/base-debian:$BUILD_TAG \
           -f Dockerfile.debian \
           --build-arg CNI_BINARIES_VERSION=$CNI_BINARIES_VERSION \
           --build-arg SURICATA_VERSION=$SURICATA_VERSION \
           --build-arg BUILD_TAG=$BUILD_TAG .
elif [ "$DISTRO" == "photon" ]; then
    if [ "$RPM_REPO_URL" == "" ] && ! ${USE_PUBLIC_PHOTON} ; then
        echoerr "Must specify --rpm-repo-url or --use-public-photon"
        exit 1
    fi
    if [ "$RPM_REPO_URL" != "" ] && ${USE_PUBLIC_PHOTON} ; then
        echoerr "Cannot specify both --rpm-repo-url and --use-public-photon"
        exit 1
    fi
    docker build $PLATFORM_ARG \
           --cache-from antrea/cni-binaries:$CNI_BINARIES_VERSION \
           --cache-from antrea/base-photon:$BUILD_TAG \
           -t antrea/base-photon:$BUILD_TAG \
           -f Dockerfile.photon \
           --build-arg CNI_BINARIES_VERSION=$CNI_BINARIES_VERSION \
           --build-arg SURICATA_VERSION=$SURICATA_VERSION \
           --build-arg RPM_REPO_URL=$RPM_REPO_URL \
           --build-arg BUILD_TAG=$BUILD_TAG .
fi

if $PUSH; then
    docker push antrea/cni-binaries:$CNI_BINARIES_VERSION
    docker push antrea/base-$DISTRO:$BUILD_TAG
fi

popd > /dev/null
