#!/bin/sh

if [ $# -lt 3 ]
  then
    echo "usage: survey.sh <url> <server count> <system credentials>"
    exit 1
fi

export NATS_SURVEYOR_SERVERS=$1
export NATS_SURVEYOR_SERVER_COUNT=$2
export NATS_SURVEYOR_CREDS=$3
docker-compose up

