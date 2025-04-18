#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

PROG=$(basename $0)

function usage {
    cat <<EOF
${PROG} [ --branch <branch> ] [ --dev <github username> ]

Options:
    --branch <branch>           Specify a branch to pull from (default: main)
    --dev <github username>     Specify a github user for developer replace, for WIP dev

For WIP dev work, to resync against the wip-dev-work-x branch in the github.com/myuserid/oran-o2ims fork, run:
hack/resync-oran-o2ims-api.sh --dev myuserid --branch wip-dev-work-x

EOF
}

#
# Defaults
#
declare BRANCH="main"
declare DEVELOPER=

#
# Process command-line arguments
#
LONGOPTS="help,branch:,dev:"
OPTS=$(getopt -o "hb:d:" --long "${LONGOPTS}" --name "$0" -- "$@")

if [ $? -ne 0 ]; then
    usage
    exit 1
fi

eval set -- "${OPTS}"

while :; do
    case "$1" in
        -b|--branch)
            BRANCH=$2
            shift 2
            ;;
        -d|--dev)
            DEVELOPER=$2
            shift 2
            ;;
        --)
            shift
            break
            ;;
        *)
            usage
            ;;
    esac
done

cmd="go get github.com/openshift-kni/oran-o2ims/api/hardwaremanagement@${BRANCH}"

if [ -n "${DEVELOPER}" ]; then
    cmd="go mod edit -replace github.com/openshift-kni/oran-o2ims/api/hardwaremanagement=github.com/${DEVELOPER}/oran-o2ims/api/hardwaremanagement@${BRANCH}"
fi

# Remove any stale replace
go mod edit -dropreplace github.com/openshift-kni/oran-o2ims/api/hardwaremanagement

echo "Running command: ${cmd}"
if ! bash -c "${cmd}"; then
    echo "Command failed" >&2
    exit 1
fi

go mod tidy && go mod vendor

