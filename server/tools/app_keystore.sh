#!/usr/bin/env bash
# app_keystore.sh , the app's signing key, made once: the keystore the release APKs are signed with,
# and ~/.config/localghost/release.env telling tools/cut_release.sh where it is.
#
#   ./tools/app_keystore.sh                        a new one: ~/.config/localghost/localghost-release.jks, alias localghost
#   ./tools/app_keystore.sh --use <file.jks> [--alias <alias>]
#                                                  the keystore you already have: nothing is made, release.env
#                                                  points the cut at it (its only key, or --alias picks one)
#   ./tools/app_keystore.sh ... --store-pass       also keeps the password in release.env (mode 600), so the
#                                                  cut never asks; without it apksigner asks at every cut
#
# The keystore is the app's identity for good: Android installs an update only when it is signed by
# the same key as the app already on the phone, so a lost keystore means every phone has to remove
# the app and install it afresh, and a leaked one lets someone else sign an "update". Keep a copy of
# the .jks (and the password) somewhere safe and off this machine. The script refuses to overwrite
# one that exists. The certificate's SHA-256 is what the app shows under VERIFY BUILD and what
# APP.txt in every release records.
set -euo pipefail

DIR="$HOME/.config/localghost"
KS="${LG_KEYSTORE:-$DIR/localghost-release.jks}"
ALIAS="${LG_KEY_ALIAS:-}"
ENVF="$DIR/release.env"
KEEP_PASS=""
USE=""
while [ $# -gt 0 ]; do
    case "$1" in
        --store-pass) KEEP_PASS=1; shift ;;
        --use) USE="${2:?--use needs the keystore file}"; shift 2 ;;
        --alias) ALIAS="${2:?--alias needs a name}"; shift 2 ;;
        *) echo "unknown option $1 (--use <file.jks> | --alias <alias> | --store-pass)" >&2; exit 2 ;;
    esac
done
command -v keytool >/dev/null 2>&1 || { echo "no keytool (it comes with the JDK: sudo apt install openjdk-17-jdk-headless)" >&2; exit 2; }
mkdir -p "$DIR"
chmod 700 "$DIR"

if [ -n "$USE" ]; then
    # the keystore you have: check the alias is in it, then point the cut at it
    [ -s "$USE" ] || { echo "no keystore at $USE" >&2; exit 1; }
    KS="$(cd "$(dirname "$USE")" && pwd)/$(basename "$USE")"
    read -rsp "its password: " P1; echo
    export LG_KEYSTORE_PASS="$P1"; unset P1
    KEYS="$(keytool -list -keystore "$KS" -storepass:env LG_KEYSTORE_PASS 2>/dev/null | sed -n 's/^\([^,]*\),.*PrivateKeyEntry.*/\1/p')" ||
        { echo "could not open $KS with that password" >&2; exit 1; }
    [ -n "$KEYS" ] || { echo "no private key in $KS (wrong password, or not a keystore)" >&2; exit 1; }
    if [ -z "$ALIAS" ]; then
        # one key: that is the one; more: say which
        if [ "$(printf '%s\n' "$KEYS" | wc -l)" -eq 1 ]; then
            ALIAS="$KEYS"
        else
            echo "$KS holds more than one key; --alias <name> picks one of:" >&2
            printf '  %s\n' $KEYS >&2
            exit 1
        fi
    elif ! printf '%s\n' "$KEYS" | grep -qx -- "$ALIAS"; then
        echo "no key '$ALIAS' in $KS. Its keys:" >&2
        printf '  %s\n' $KEYS >&2
        exit 1
    fi
else
    ALIAS="${ALIAS:-localghost}"
    if [ -s "$KS" ]; then
        echo "a keystore is already at $KS: not touching it (it is the app's identity; a new one would be a new app to every phone). --use $KS points the cut at it." >&2
        exit 1
    fi
    # the password, typed twice, never on a command line
    read -rsp "keystore password (8 or more characters): " P1; echo
    read -rsp "again: " P2; echo
    [ "$P1" = "$P2" ] || { echo "the two do not match" >&2; exit 1; }
    [ "${#P1}" -ge 8 ] || { echo "8 or more characters" >&2; exit 1; }
    export LG_KEYSTORE_PASS="$P1"
    unset P1 P2
    # RSA 4096, a hundred years: one key, one store password (PKCS12 keeps them the same)
    keytool -genkeypair -v -keystore "$KS" -storetype PKCS12 -alias "$ALIAS" -keyalg RSA -keysize 4096 \
        -validity 36500 -dname "CN=LocalGhost, O=LocalGhost, C=GB" \
        -storepass:env LG_KEYSTORE_PASS -keypass:env LG_KEYSTORE_PASS >/dev/null
    chmod 600 "$KS"
fi

{
    echo "# the app's signing key, for tools/cut_release.sh (made by tools/app_keystore.sh $(date -u +%Y-%m-%d))"
    echo "LG_KEYSTORE=$KS"
    echo "LG_KEY_ALIAS=$ALIAS"
    if [ -n "$KEEP_PASS" ]; then
        printf 'LG_KEYSTORE_PASS=%q\n' "$LG_KEYSTORE_PASS"
    else
        echo "# LG_KEYSTORE_PASS=   (set it to stop apksigner asking at every cut; this file is mode 600)"
    fi
    echo "# LG_KEY_PASS=        (only when the key's password differs from the store's, as Android Studio allows)"
    [ -n "${ANDROID_HOME:-}" ] && echo "ANDROID_HOME=$ANDROID_HOME"
    [ -n "${JAVA_HOME:-}" ] && echo "JAVA_HOME=$JAVA_HOME"
    true
} > "$ENVF"
chmod 600 "$ENVF"

echo "keystore: $KS (alias $ALIAS)"
echo "env:      $ENVF"
keytool -list -v -keystore "$KS" -alias "$ALIAS" -storepass:env LG_KEYSTORE_PASS 2>/dev/null | grep -E 'SHA256:|Valid from' | sed 's/^[[:space:]]*/  /'
echo
[ -n "$USE" ] || echo "back it up now, off this machine: $KS and the password."
echo "then: ./tools/cut_release.sh <version>"
