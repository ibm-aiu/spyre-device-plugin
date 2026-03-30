#!/bin/sh
# +-------------------------------------------------------------------+
# | (C) Copyright IBM Corp. 2025, 2026                                |
# | SPDX-License-Identifier: Apache-2.0                               |
# +-------------------------------------------------------------------+

if [[ -z "${PLUGIN_PATH+x}" ]]; then
	PLUGIN_PATH="/usr/local/etc/device-plugins"
fi

COMPLETE_FILE=$PLUGIN_PATH/complete
OUTPUT_PATH=$PLUGIN_PATH/metadata

mkdir -p $OUTPUT_PATH

echo PLUGIN_PATH=$PLUGIN_PATH
echo COMPLETE_FILE=$COMPLETE_FILE
echo OUTPUT_PATH=$OUTPUT_PATH

if [[ "${PSEUDO_DEVICE_MODE}" -eq "1" ]]; then
	echo "Use copied pseudo topology file"
	cp /pseudo-topology.json $OUTPUT_PATH/topo.json
	exit 0
fi

if [[ -z "${SKIP_IF_COMPLETED+x}" ]]; then
	SKIP_IF_COMPLETED="true"
fi

# VERIFY_P2P=1 to enable --verify-p2p
# Disable if not set because getting `Error: --verify-p2p not supported in VF mode.`
# even with DOOM.enable=false.
if [[ -z "${VERIFY_P2P+x}" ]]; then
	VERIFY_P2P="0"
fi

echo SKIP_IF_COMPLETED=$SKIP_IF_COMPLETED
echo TOOLBOX_BIN=$TOOLBOX_BIN
echo VERIFY_P2P=$VERIFY_P2P

# Check if SKIP_IF_COMPLETED is "true"
if [[ "$SKIP_IF_COMPLETED" == "true" ]]; then
	# Now check if the COMPLETE_FILE exists
	if [[ -f "$COMPLETE_FILE" ]]; then
		echo "Complete file exists: $COMPLETE_FILE, skip"
		exit 0
	fi
fi

# Generate senlib_config.json

echo "Discovering pci device list"
# No need to execute verify-p2p at this step
export AIU_DISCOVER_AIU_VERIFY_P2P="0"
$TOOLBOX_BIN/aiu-discover-topo -j /tmp/topo.json

# Extract the value
devices=$(cat /tmp/topo.json | jq .devices)

# Check if it's null or empty
if [ "$devices" = "null" ] || [ -z "$devices" ]; then
	echo "WARNING: devices field is null or empty"
	pcis=[]
else
	pcis=[$(echo $devices | jq 'keys | .[]' | awk 'NR > 1 { printf(",") } {printf "%s",$0}')]
fi

mkdir -p /etc/aiu
echo "Prepare senlib_config.json and environment to verify topology and get metadata"
cat /gen-topo-template.json | jq .GENERAL.sen_bus_id=$pcis >/etc/aiu/senlib_config.json

echo "Run discover-topo"
export AIU_DISCOVER_AIU_VERIFY_P2P=$VERIFY_P2P
export SENLIB_DEVEL_CONFIG_FILE=/etc/aiu/senlib_config.json
echo AIU_DISCOVER_AIU_VERIFY_P2P=$AIU_DISCOVER_AIU_VERIFY_P2P
# Generate topology file at $OUTPUT_PATH/topo.json
$TOOLBOX_BIN/aiu-discover-topo --aiu-metadata -v \
	--json $OUTPUT_PATH/topo.json

echo "1" >$COMPLETE_FILE
