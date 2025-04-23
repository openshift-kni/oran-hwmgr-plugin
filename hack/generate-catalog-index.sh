#!/bin/bash
#
# SPDX-FileCopyrightText: Red Hat
#
# SPDX-License-Identifier: Apache-2.0
#
# Generate catalog index
#

declare WORKDIR=

function usage {
    cat <<EOF
Usage: $0 -o <opm-executable> -n <package-name> -c <channel> -v <version>
EOF
    exit 1
}

function cleanup {
    if [ -n "${WORKDIR}" ] && [ -d "${WORKDIR}" ]; then
        rm -rf "${WORKDIR}"
    fi
}

trap cleanup EXIT

#
# Process cmdline arguments
#
declare OPM=
declare NAME=
declare CHANNEL=
declare VERSION=

while getopts ":o:n:c:v:" opt; do
    case "${opt}" in
        o)
            OPM="${OPTARG}"
            ;;
        n)
            NAME="${OPTARG}"
            ;;
        c)
            CHANNEL="${OPTARG}"
            ;;
        v)
            VERSION="${OPTARG}"
            ;;
        *)
            usage
            ;;
    esac
done

if [ -z "${OPM}" ] || [ -z "${NAME}" ] || [ -z "${CHANNEL}" ] || [ -z "${VERSION}" ]; then
    usage
fi

WORKDIR=$(mktemp -d --tmpdir genindex.XXXXXX)

${OPM} init ${NAME} --default-channel=${CHANNEL} --output=yaml > ${WORKDIR}/index.yaml
cat <<EOF >> ${WORKDIR}/index.yaml
---
schema: olm.channel
package: ${NAME}
name: ${CHANNEL}
entries:
  - name: ${NAME}.v${VERSION}
EOF

if [ ! -f catalog/index.yaml ] || ! cmp ${WORKDIR}/index.yaml catalog/index.yaml; then
    mv ${WORKDIR}/index.yaml catalog/index.yaml
fi

