#!/usr/bin/env bash

set -e

. "$(dirname "$0")"/common.sh

GOLANGCI_LINT_VERSION="1.64.8"
# This is required because the Openshift CI is running this test as a non-root
# user but whitin the / directory as the home directory, therefore, the linter
# won't have the permissions to create the `/.cache` directory.
export GOCACHE=/tmp
export GOLANGCI_LINT_CACHE=/tmp

# IsGoLangCiLintInstalled is used to check whether golangci-lint executable is on the $PATH.
function IsGolangCiLintInstalled() {

	echo "Checking if golangci-lint is installed"

	if which golangci-lint &> /dev/null; then
		return 0
	fi

	echo "Could not find golangci-lint on path"

	return 1
}

# IsGolangCiLintCorrectVersion is used to check the installed version of golangci-lint and ensure it is as expected.
function IsGolangCiLintCorrectVersion() {

	local requiredVersion
	requiredVersion="${1}"

	local installedVersion
	installedVersion="$(golangci-lint version 2> /dev/null | awk '{ print $4 }')"
	echo "Checking if installed golang version matches requirement"

	if [[ "${installedVersion}" == "${requiredVersion}" ]]; then
		return 0
	fi

	echo "Installed version does not match expected version"

	return 1
}

# DownloadGolangCiLint is used to download a specified version of golangci-lint
function DownloadGolangCiLint() {

	local versionNumber
	versionNumber="${1}"

	echo "installing golangci-lint version ${versionNumber}"
	if curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v${versionNumber}; then
		return 0
	fi

	return 1
}

# RunGolangCiLint is used to execute the lint command
function RunGolangCiLint() {

	echo "Running golangci-lint"

	if golangci-lint run -v --timeout 10m; then
		return 0;
	fi

	return 1
}

# Main body of shell script
function Main() {

	# Check whether we need to install golangci-lint
	local installRequired
	installRequired="false"

	# Check if its present at all
	if ! IsGolangCiLintInstalled; then
		installRequired="true"
	fi

	# Check if its the right version
	if ! IsGolangCiLintCorrectVersion "${GOLANGCI_LINT_VERSION}"; then
		installRequired="true"
	fi

	# Install the correct version if we need to
	if [[ "${installRequired}" == "true" ]]; then
		if ! DownloadGolangCiLint "${GOLANGCI_LINT_VERSION}"; then
			echo "failed to install golangci-lint"
			return 1
		fi
	else
		echo "golangci-lint already installed and correct version (${GOLANGCI_LINT_VERSION})"
	fi

	# Execute the linter
	if ! RunGolangCiLint; then
		echo "failed to pass linter checks"
		return 1
	fi

	# All passed!
	return 0
}

if Main; then
	exit 0
fi

exit 1
