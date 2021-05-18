#!/bin/sh

# sudo docker run --rm=true -itv $PWD:/mnt golang:buster /mnt/build_bin_docker.sh

cd /mnt
go version
go build -ldflags="-s -w" -o telefonist *.go