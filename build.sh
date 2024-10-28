#!/bin/sh

GOOS=$1
GOARCH=$2
GOOS=$GOOS GOARCH=$GOARCH go build -o ./ ./...

if [[ "$GOOS" = "windows" ]]
then
    BIN_FILE="rearm-cli.exe"
else
    BIN_FILE="rearm-cli"
fi

zip -r -j ../$VERSION/rearm-cli-$VERSION-$GOOS-$GOARCH.zip ./$BIN_FILE