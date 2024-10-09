#!/bin/sh

GOOS=$1
GOARCH=$2
GOOS=$GOOS GOARCH=$GOARCH go build -o ./ ./...

if [[ "$GOOS" = "windows" ]]
then
    BIN_FILE="reliza-cli.exe"
else
    BIN_FILE="reliza-cli"
fi

zip -r -j ../$VERSION/reliza-cli-$VERSION-$GOOS-$GOARCH.zip ./$BIN_FILE