#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#

PROG=$(basename $0)

function usage {
    cat <<EOF
${PROG} [ -b <branch> ] [ -d <github username> ]

Options:
    -b <branch>           Specify a branch to pull from (default: main)
    -d <github username>  Specify a github user for developer replace, for WIP dev

For WIP dev work, to resync against the wip-dev-work-x branch in the github.com/myuserid/oran-o2ims fork, run:
hack/resync-oran-o2ims-api.sh --dev myuserid --branch wip-dev-work-x

EOF
    exit 1
}

#
# Defaults
#
declare BRANCH="main"
declare DEVELOPER=

#
# Process command-line arguments
#
while getopts ":b:d:" opt; do
    case "${opt}" in
        b)
            BRANCH=${OPTARG}
            ;;
        d)
            DEVELOPER=${OPTARG}
            ;;
        *)
            echo "opt=${opt}"
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

