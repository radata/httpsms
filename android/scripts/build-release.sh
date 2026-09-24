#!/bin/sh
# Build a versioned httpSMS *Android app* release.
#
#   ./scripts/build-release.sh             debug apk      -> archive + public download
#   ./scripts/build-release.sh --release   signed apk     -> archive + public download
#   ./scripts/build-release.sh --bundle    signed aab     -> archive only (Google Play)
#
# Output: android/ops/tmp/httpsms_android_<variant>_vc<versionCode>_<commit>_<timestamp>.tar.gz
#           release.json
#           app-<variant>.apk | app-release.aab
#         android/ops/public/HttpSms.apk          <- the published download
#         android/ops/public/HttpSms.apk.sha256
#
# THE SCRIPT LIVES IN scripts/, ITS OUTPUT IN ops/. scripts/ is local tooling
# and is tracked; ops/ is gitignored (**/ops/) and ops/public is what the web
# container bind-mounts at /downloads. Writing next to the script would put
# 50MB tarballs and an apk into a tracked directory of a PUBLIC repo, and leave
# the served download stale.
#
# VERSION: android/version.properties, hand-bumped. It is both versionCode and
# versionName. Play rejects a versionCode it has already seen, so when --bundle
# finds the current one already archived here it offers to bump it (Y/n).
#
# NOTE THE ASYMMETRY WITH api/ AND web/: there is no server half and no
# deploy.sh. The artifact is an APK you sideload onto the phone (or an AAB you
# upload to Play Console), so the release ends at the tarball.
#
# TWO OUTPUTS, TWO JOBS. tmp/ holds the ARCHIVE: versioned, accumulating, the
# thing you roll back to. It is not installable as it stands — the tarball has
# to be unpacked first and the apk inside carries a build-specific name.
# public/ holds the DOWNLOAD: one apk under a name that never changes, so
# APP_DOWNLOAD_URL can point at a single URL for good while the build behind it
# moves. The web container bind-mounts public/ at /downloads (web/nginx.conf,
# web/docker-compose.yml).
#
# EVERYTHING IN public/ IS SERVED TO THE INTERNET. That is why it is a separate
# directory rather than tmp/ being mounted directly: what gets published is what
# this script deliberately put there, not whatever earlier builds left lying
# around.
#
# WHY THERE IS A HARD GATE BELOW
#
# app/google-services.json ships pointing at the UPSTREAM Firebase project
# (httpsms-86c51). An APK built against it authenticates to someone else's
# project and receives tokens this deployment's API will reject with a 401 —
# and nothing in the build, the install, or the app's own UI says why. The
# ordering "replace the json, THEN build" is therefore enforced here instead of
# being left to memory.
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ANDROID_DIR="$(dirname "$SCRIPT_DIR")"
REPO_DIR="$(dirname "$ANDROID_DIR")"
OPS_DIR="$ANDROID_DIR/ops"

SERVICE="httpsms_android"
UPSTREAM_PROJECT_ID="httpsms-86c51"

VARIANT="debug"
KIND="debug"
GRADLE_TASK="assembleDebug"
for arg in "$@"; do
  case "$arg" in
    --release)
      VARIANT="release"
      KIND="release"
      GRADLE_TASK="assembleRelease"
      ;;
    --bundle)
      # Play only takes an Android App Bundle for new apps. An aab is not
      # installable on a phone, so this mode archives and does not publish.
      VARIANT="release"
      KIND="bundle"
      GRADLE_TASK="bundleRelease"
      ;;
    *)
      echo "Unknown argument: $arg"
      echo "Usage: ./scripts/build-release.sh [--release | --bundle]"
      exit 1
      ;;
  esac
done

echo "🚀 Building httpSMS Android release..."
echo ""

# ── Gate: the Firebase config must be YOURS ───────────────────────────────────

GS_JSON="$ANDROID_DIR/app/google-services.json"

if [ ! -f "$GS_JSON" ]; then
  echo "❌ Missing $GS_JSON"
  echo "   Download it from the Firebase console for YOUR project:"
  echo "     Project settings -> Your apps -> Android -> google-services.json"
  echo "   The app package is com.httpsms."
  exit 1
fi

PROJECT_ID=$(sed -n 's/.*"project_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$GS_JSON" | head -1)

if [ -z "$PROJECT_ID" ]; then
  echo "❌ Could not read project_id from $GS_JSON — is it valid JSON?"
  exit 1
fi

if [ "$PROJECT_ID" = "$UPSTREAM_PROJECT_ID" ]; then
  echo "❌ app/google-services.json still points at the UPSTREAM Firebase project."
  echo ""
  echo "     project_id: $PROJECT_ID   (this is NdoleStudio's, not yours)"
  echo ""
  echo "   An APK built from this would sign in against someone else's Firebase"
  echo "   project, and every request to your API would come back 401 with no"
  echo "   explanation anywhere."
  echo ""
  echo "   Fix, in order:"
  echo "     1. Firebase console -> your project -> Project settings"
  echo "     2. Your apps -> Add app -> Android, package name: com.httpsms"
  echo "     3. Download google-services.json"
  echo "     4. Replace $GS_JSON"
  echo "     5. Run this script again"
  exit 1
fi

# Optional exact assertion. Export HTTPSMS_FIREBASE_PROJECT_ID to make the build
# fail on any project but the one you name — worth doing in CI, or once more
# than one Firebase project exists on this machine.
if [ -n "$HTTPSMS_FIREBASE_PROJECT_ID" ] && [ "$PROJECT_ID" != "$HTTPSMS_FIREBASE_PROJECT_ID" ]; then
  echo "❌ google-services.json is for project '$PROJECT_ID',"
  echo "   but HTTPSMS_FIREBASE_PROJECT_ID says it must be '$HTTPSMS_FIREBASE_PROJECT_ID'."
  exit 1
fi

echo "🔑 Firebase project: $PROJECT_ID"

# ── Gate: applicationId must be registered in that project ────────────────────
#
# The google-services gradle plugin matches app/build.gradle.kts's applicationId
# against the package_name of a client in google-services.json. A mismatch fails
# the build with "No matching client found for package name '<id>'", several
# minutes into gradle and with no hint about which of the two is wrong. Check it
# here, in a second, while both values are in front of you.
#
# NOTE this is applicationId, NOT namespace. namespace is the package for the
# generated R/BuildConfig classes and tracks the Kotlin source tree; Firebase
# never sees it, and the two are allowed to differ.

APP_ID=$(sed -n -E 's/.*applicationId[[:space:]]*(=[[:space:]]*)?"([^"]*)".*/\2/p' \
           "$ANDROID_DIR/app/build.gradle.kts" | head -1)
JSON_PACKAGES=$(sed -n 's/.*"package_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
                  "$GS_JSON" | sort -u)

if [ -z "$APP_ID" ]; then
  echo "❌ Could not read applicationId from app/build.gradle.kts"
  exit 1
fi

if ! printf '%s\n' "$JSON_PACKAGES" | grep -qx "$APP_ID"; then
  echo "❌ applicationId is not registered in this Firebase project."
  echo ""
  echo "     applicationId (build.gradle.kts): $APP_ID"
  echo "     package_name  (google-services) : $(printf '%s' "$JSON_PACKAGES" | tr '\n' ' ')"
  echo ""
  echo "   gradle would fail later with \"No matching client found for package"
  echo "   name '$APP_ID'\". Make them agree — either:"
  echo "     • set applicationId to one of the package names above, or"
  echo "     • register '$APP_ID' as an Android app in project '$PROJECT_ID'"
  echo "       and download a fresh google-services.json"
  exit 1
fi

echo "📱 applicationId:    $APP_ID"

# ── Warn: the default server is upstream's ────────────────────────────────────
#
# Not fatal. The login screen accepts a custom server URL, so an APK with the
# upstream default still works if you type yours. Worth saying out loud though,
# because "it silently talks to api.httpsms.com" is not a failure anyone enjoys
# diagnosing from a phone.

if ! command -v java >/dev/null 2>&1; then
  echo "❌ java not found on PATH."
  echo "   brew install --cask temurin@17"
  echo "   or: export JAVA_HOME=\"/Applications/Android Studio.app/Contents/jbr/Contents/Home\""
  exit 1
fi

STRINGS_XML="$ANDROID_DIR/app/src/main/res/values/strings.xml"
if grep -q 'name="default_server_url">https://api.httpsms.com' "$STRINGS_XML" 2>/dev/null; then
  echo "⚠️  default_server_url is still https://api.httpsms.com (upstream's)."
  echo "    The login screen lets you type your own server, so this is not fatal."
  echo "    To change the default, edit:"
  echo "      $STRINGS_XML"
  echo ""
fi

# ── Gate: release builds must be signed ───────────────────────────────────────
#
# app/build.gradle.kts deliberately leaves the release build UNSIGNED when no
# credentials are found, so that `test` configures on any machine. That is
# right for gradle and wrong here: an unsigned apk will not install and Play
# rejects an unsigned aab. Catch it before gradle runs.

if [ "$VARIANT" = "release" ] \
   && ! grep -qs '^storeFile=' "$ANDROID_DIR/keystore.properties" \
   && [ -z "$HW_SMS_STORE_FILE" ]; then
  echo "❌ No release signing credentials."
  echo "   Create android/keystore.properties (storeFile, storePassword, keyAlias,"
  echo "   keyPassword) or export HW_SMS_STORE_FILE and friends."
  exit 1
fi

# ── Version ───────────────────────────────────────────────────────────────────

VERSION_CODE=$(sed -n 's/^versionCode[[:space:]]*=[[:space:]]*\([0-9][0-9]*\).*/\1/p' \
                 "$ANDROID_DIR/version.properties" | head -1)
if [ -z "$VERSION_CODE" ]; then
  echo "❌ Could not read versionCode from android/version.properties"
  exit 1
fi

RELEASE_DIR="$OPS_DIR/tmp"

# ls rather than a glob test: sh has no nullglob, and an unmatched pattern
# would be taken literally.
bundled() {
  ls "$RELEASE_DIR"/${SERVICE}_bundle_vc$1_*.tar.gz 2>/dev/null | head -1
}

if [ "$KIND" = "bundle" ]; then
  PREVIOUS=$(bundled "$VERSION_CODE")
  if [ -n "$PREVIOUS" ]; then
    # Skip past every code already archived, not just the current one: Play
    # accepts gaps, it only rejects a number it has seen.
    NEXT=$((VERSION_CODE + 1))
    while [ -n "$(bundled "$NEXT")" ]; do NEXT=$((NEXT + 1)); done

    echo "⚠️  versionCode $VERSION_CODE was already bundled:"
    echo "     $(basename "$PREVIOUS")"
    echo "   Play rejects a versionCode it has seen."

    # Only ask on a terminal. Piped or in CI there is nobody to answer, and a
    # silent bump there would be a surprise diff.
    ANSWER=""
    if [ -t 0 ]; then
      printf "   Bump android/version.properties to %s? [Y/n] " "$NEXT"
      read -r ANSWER
    fi

    case "$ANSWER" in
      ""|y|Y|yes|YES)
        if [ ! -t 0 ]; then
          echo "   Not a terminal — bump android/version.properties to $NEXT and run again."
          exit 1
        fi
        sed -i.bak "s/^versionCode[[:space:]]*=.*/versionCode=${NEXT}/" \
          "$ANDROID_DIR/version.properties"
        rm -f "$ANDROID_DIR/version.properties.bak"
        VERSION_CODE="$NEXT"
        echo "🔢 Bumped versionCode to $VERSION_CODE (not committed)."
        ;;
      *)
        echo "   Not bumped. (Never uploaded $VERSION_CODE? Delete that tarball.)"
        exit 1
        ;;
    esac
  fi
fi

cd "$REPO_DIR"

GIT_COMMIT=$(git rev-parse --short=12 HEAD 2>/dev/null || echo "unknown")
GIT_DIRTY=""
if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
  GIT_DIRTY="-dirty"
fi

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
VERSION="${GIT_COMMIT}${GIT_DIRTY}"
RELEASE_NAME="${SERVICE}_${KIND}_vc${VERSION_CODE}_${VERSION}_${TIMESTAMP}"

echo "📦 Release: ${RELEASE_NAME}"
echo "🔢 Version: ${VERSION_CODE}"
echo "🤖 Variant: ${VARIANT}"

if [ "$KIND" = "bundle" ] && [ -n "$GIT_DIRTY" ]; then
  echo "⚠️  Working tree is dirty — this upload will not match any commit."
  echo "    Commit (at least the version.properties bump) first if you can."
fi

# ── Build ─────────────────────────────────────────────────────────────────────

DIST="$OPS_DIR/dist"
rm -rf "$DIST"
mkdir -p "$DIST"

echo ""
echo "🔨 Building (gradle ${GRADLE_TASK})..."

cd "$ANDROID_DIR"

# Invoked via `sh` on purpose. The gradle wrapper arrives without its execute
# bit from some checkouts (it did here), and `./gradlew` then dies with
# "Permission denied" after the Firebase gate has already passed — a confusing
# place to fail. gradlew is a POSIX shell script, so this works either way and
# needs nothing kept in sync.
sh ./gradlew --no-daemon "$GRADLE_TASK"

if [ "$KIND" = "bundle" ]; then
  OUT_SUBDIR="bundle/$VARIANT"; OUT_EXT="aab"
else
  OUT_SUBDIR="apk/$VARIANT";    OUT_EXT="apk"
fi

APK=$(find "$ANDROID_DIR/app/build/outputs/$OUT_SUBDIR" -name "*.$OUT_EXT" -type f 2>/dev/null | head -1)
if [ -z "$APK" ]; then
  echo "❌ gradle reported success but no .$OUT_EXT was found under"
  echo "   app/build/outputs/$OUT_SUBDIR"
  exit 1
fi

cp "$APK" "$DIST/$(basename "$APK")"

# ── Package ───────────────────────────────────────────────────────────────────

cat > "$DIST/release.json" <<EOF
{
  "service": "${SERVICE}",
  "version": "${VERSION}",
  "version_code": ${VERSION_CODE},
  "timestamp": "${TIMESTAMP}",
  "release_name": "${RELEASE_NAME}",
  "git_commit": "${GIT_COMMIT}",
  "variant": "${VARIANT}",
  "kind": "${KIND}",
  "artifact": "$(basename "$APK")",
  "firebase_project_id": "${PROJECT_ID}",
  "build_date": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
}
EOF

mkdir -p "$RELEASE_DIR"
tar -czf "${RELEASE_DIR}/${RELEASE_NAME}.tar.gz" -C "$DIST" .
rm -rf "$DIST"

# ── Publish ───────────────────────────────────────────────────────────────────
#
# Copy, do not symlink. This directory is bind-mounted into the web container,
# and a symlink resolves against the CONTAINER's filesystem — where the target
# path does not exist. nginx would answer 404 for a file that is plainly there
# on the host.

if [ "$KIND" = "bundle" ]; then
  echo ""
  echo "✅ Archived: android/ops/tmp/${RELEASE_NAME}.tar.gz"
  echo "📤 Upload to Play Console (versionCode ${VERSION_CODE}):"
  echo "   $APK"
  echo "   Play Console -> your app -> Test and release -> <track> -> Create new release"
  echo ""
  echo "   ops/public was left alone: an aab does not install on a phone."
  exit 0
fi

PUBLIC_DIR="$OPS_DIR/public"
PUBLISH_NAME="HttpSms.apk"

mkdir -p "$PUBLIC_DIR"
cp "$APK" "$PUBLIC_DIR/$PUBLISH_NAME"

# A checksum, because the whole point of this file is that people install it on
# a phone from a URL. shasum is what macOS ships, sha256sum is what Alpine and
# Debian ship, and neither box has both.
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$PUBLIC_DIR" && sha256sum "$PUBLISH_NAME" > "${PUBLISH_NAME}.sha256")
elif command -v shasum >/dev/null 2>&1; then
  (cd "$PUBLIC_DIR" && shasum -a 256 "$PUBLISH_NAME" > "${PUBLISH_NAME}.sha256")
else
  echo "⚠️  No sha256sum or shasum on PATH — publishing the apk without a checksum."
fi

# NO release.json HERE, deliberately. The archive in tmp/ carries one and that is
# where build metadata belongs. This directory is world-readable over HTTP, and
# the commit, build host timestamps and Firebase project id have no reader on the
# public side — only the apk and a checksum do.

echo ""
echo "✅ Archived:  android/ops/tmp/${RELEASE_NAME}.tar.gz"
echo "📥 Published: android/ops/public/${PUBLISH_NAME}"
echo ""

# ── The debug-build warning ───────────────────────────────────────────────────
#
# Worth saying loudly at the END, where it is read. A debug apk IS signed — it
# has to be, Android installs nothing unsigned — but it is signed by
# CN=Android Debug, O=Android, C=US, from the keystore gradle generates at
# ~/.android/debug.keystore. That key is not shared between machines, but it is
# stored with the publicly documented password "android", it is never backed up,
# and the build sets android:debuggable.
#
# THE PART THAT BITES LATER: Android only accepts an update signed with the SAME
# key as the installed app. Hand this apk to people and every one of them has to
# uninstall and reinstall the day you switch to a real release key — losing the
# stored API key and settings with it.
#
# Fine over adb to your own phone. Not fine on a public URL, which is exactly
# what publishing it here makes it.

if [ "$VARIANT" = "debug" ]; then
  echo "⚠️  THIS IS A DEBUG BUILD, and it has just been staged for public download."
  echo "    It is signed by CN=Android Debug (~/.android/debug.keystore,"
  echo "    password \"android\") and is marked debuggable — anyone with adb on the"
  echo "    device can read the stored httpSMS api key."
  echo "    Anyone you give it to must UNINSTALL to move to a real release build:"
  echo "    Android rejects an update signed with a different key."
  echo "    Use --release (signed with the keystore in keystore.properties)."
  echo ""
fi

echo "📤 Install directly:"
echo "   adb install -r android/ops/public/${PUBLISH_NAME}"
echo ""
echo "🌐 To serve it, the web container mounts ops/public at /downloads."
echo "   In web/.env (APK_DIR already defaults to ../android/ops/public in dev):"
echo "     APP_DOWNLOAD_URL=<your APP_URL>/downloads/${PUBLISH_NAME}"
echo "   then: docker compose up -d --force-recreate web"
