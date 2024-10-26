#!/bin/sh

GOOS=$1
GOARCH=$2
GOOS=$GOOS GOARCH=$GOARCH go build -o ./ ./...

if [[ "$GOOS" = "windows" ]]
then
    BIN_FILE="rearm.exe"
else
    BIN_FILE="rearm"
fi

zip -r -j ../$VERSION/rearm-$VERSION-$GOOS-$GOARCH.zip ./$BIN_FILE