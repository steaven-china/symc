#!/bin/bash
set -e
: "${REGION:=region-a}"
: "${NATS_URL:=nats://nats-0.nats:4222}"
cd /opt/paper
mkdir -p plugins/symc
if [ -f plugins/symc/config.yml.template ]; then
  REGION="$REGION" NATS_URL="$NATS_URL" \
    envsubst < plugins/symc/config.yml.template > plugins/symc/config.yml
fi
JAVA_OPTS=("-Xms2G" "-Xmx2G" "-XX:+UseG1GC" "-Dfile.encoding=UTF-8"
           "-Dnats.url=${NATS_URL}" "-Dregion.id=${REGION}")
exec java "${JAVA_OPTS[@]}" \
  -jar /opt/paper/paper-paperclip-26.1.2.local-SNAPSHOT.jar --nogui "$@"
