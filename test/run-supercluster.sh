#!/bin/bash

nats-server -config  r1s1.conf &
nats-server -config  r1s2.conf &
nats-server -config  r2s3.conf &

