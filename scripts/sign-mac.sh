#!/usr/bin/env bash
# Signs and notarises a release's Mac downloads on this Mac, with the
# Developer ID certificate in its keychain, and puts them back on the
# release in place of the unsigned ones GitHub built. The certificate never
# leaves this Mac. Run it for a draft release, then publish it:
#
#   scripts/sign-mac.sh v0.1.0
#
# It needs Xcode, or its command line tools, and gh logged in. The first time,
# it asks for your Apple ID and an app-specific password for notarising, and
# keeps them in the keychain.
set -euo pipefail

tag="${1:?say which release, as in: scripts/sign-mac.sh v0.1.0}"
repo=DuffleOne/factory-pack
profile="${NOTARY_PROFILE:-factory-pack}"

identity=$(security find-identity -v -p codesigning | awk -F'"' '/Developer ID Application/ {print $2; exit}')
if [ -z "$identity" ]; then
  echo "there's no Developer ID Application certificate in your keychain."
  echo "Xcode makes one: Settings, Accounts, your team, Manage Certificates, then + and Developer ID Application."
  exit 1
fi

echo "signing as $identity"

if ! xcrun notarytool history --keychain-profile "$profile" >/dev/null 2>&1; then
  echo
  echo "notarising needs your Apple ID and an app-specific password, from account.apple.com,"
  echo "under Sign-In and Security. they're kept in your keychain as \"$profile\", so this is once only."
  xcrun notarytool store-credentials "$profile"
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"

gh release download "$tag" --repo "$repo" --pattern FactoryPack-macOS.zip --pattern factory-pack-macos.zip --pattern SHA256SUMS

mkdir app cli
ditto -x -k FactoryPack-macOS.zip app
ditto -x -k factory-pack-macos.zip cli

sign() { codesign --force --options runtime --timestamp --sign "$identity" "$1"; }
sign "app/Factory Pack.app/Contents/MacOS/Factory Pack"
sign "app/Factory Pack.app"
sign cli/factory-pack
codesign --verify --strict --deep "app/Factory Pack.app"

notarise() {
  echo "notarising $(basename "$1"): Apple usually answers in a minute or two"
  ditto -c -k --keepParent "$1" upload.zip
  out=$(xcrun notarytool submit upload.zip --keychain-profile "$profile" --wait 2>&1) || true
  echo "$out" | sed 's/^/  /'
  rm upload.zip
  if ! echo "$out" | grep -q "status: Accepted"; then
    echo "Apple didn't accept it: \`xcrun notarytool log <id> --keychain-profile $profile\` says why"
    exit 1
  fi
}

notarise "app/Factory Pack.app"
notarise cli/factory-pack

# The app carries Apple's ticket, so it opens offline. A bare program can't;
# Gatekeeper looks it up the first time instead.
xcrun stapler staple "app/Factory Pack.app"
spctl --assess --type execute --verbose "app/Factory Pack.app"

rm FactoryPack-macOS.zip factory-pack-macos.zip
ditto -c -k --keepParent "app/Factory Pack.app" FactoryPack-macOS.zip
(cd cli && zip -qr ../factory-pack-macos.zip .)

# The checksums of these two change; the rest stay as GitHub wrote them.
grep -v -e ' FactoryPack-macOS.zip$' -e ' factory-pack-macos.zip$' SHA256SUMS > sums || true
shasum -a 256 FactoryPack-macOS.zip factory-pack-macos.zip >> sums
sort -k2 sums > SHA256SUMS

gh release upload "$tag" FactoryPack-macOS.zip factory-pack-macos.zip SHA256SUMS --repo "$repo" --clobber

echo
echo "signed, notarised and on the release. publish it when you're ready:"
echo "  gh release edit $tag --repo $repo --draft=false"
