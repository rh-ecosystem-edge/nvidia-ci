#!/usr/bin/env bash

set -e

GINKGO_PACKAGE='github.com/onsi/ginkgo/v2'

REQUIRED_GINKGO_VERSION="$(grep ${GINKGO_PACKAGE} go.mod | cut -d ' ' -f2 | cut -c 2-)"
if [[ -z "$REQUIRED_GINKGO_VERSION" ]]; then
    echo "Gingko version cannot be determined. Check the package path in go.mod"
    exit 1
fi

INSTALLED_GINKGO_VERSION="$(ginkgo version | { read -r _ _ v _; echo "${v}"; })"

GOPATH="${GOPATH:-/root/go}"
export PATH=$PATH:$GOPATH/bin

if [ "${INSTALLED_GINKGO_VERSION}" == "${REQUIRED_GINKGO_VERSION}" ]; then
    echo "Gingko tool is up to date"
else
    echo "Installing ginkgo tool"
	go install "${GINKGO_PACKAGE}/ginkgo@v${REQUIRED_GINKGO_VERSION}"
fi
