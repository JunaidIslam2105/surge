#!/bin/sh
set -eu

binary=$1
appdir=$2

mkdir -p "$appdir"
install -m 755 "$binary" "$appdir/surge"
install -m 755 packaging/appimage/AppRun "$appdir/AppRun"
install -m 644 packaging/appimage/surge.desktop "$appdir/surge.desktop"
install -m 644 assets/logo.png "$appdir/surge.png"
