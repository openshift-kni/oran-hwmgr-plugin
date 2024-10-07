#!/bin/bash

PROG=$(basename $0)

function usage {
    cat <<EOF
${PROG} [ -b <branch> ]

Options:
    -b <branch>     Specify a branch to pull from (default: main)
EOF
}

#
# Defaults
#
declare BRANCH="main"

#
# Process command-line arguments
#
LONGOPTS="help,branch:"
OPTS=$(getopt -o "hb:" --long "${LONGOPTS}" --name "$0" -- "$@")

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
        --)
            shift
            break
            ;;
        *)
            usage
            ;;
    esac
done

if ! go get "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement@${BRANCH}"; then
    echo "go get request failed" >&2
    exit 1
fi

go mod tidy && go mod vendor

