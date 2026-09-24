#!/bin/sh
# Drive an attached device in and out of Doze, to test what httpSMS does when a
# phone has been sitting on a desk for hours.
#
# Usage:
#   ./ops/doze-test.sh idle3     3h idle, app ON the allowlist — SHIPPED config
#   ./ops/doze-test.sh idle      same thing — 3h is the default
#   ./ops/doze-test.sh idle24    a full day
#   ./ops/doze-test.sh idle3 --no-allowlist          the un-exempt user
#   ./ops/doze-test.sh idle3 --no-allowlist --bucket restricted   worst case
#   ./ops/doze-test.sh normal    undo all of it, restore what was there before
#   ./ops/doze-test.sh status    what state is the device in right now
#   ./ops/doze-test.sh step      advance ONE Doze stage instead of jumping
#   ./ops/doze-test.sh logs      follow the app + FCM log
#
# WHY THIS IS NOT JUST `dumpsys deviceidle force-idle`
#
# This app is normally on the battery-optimisation whitelist — it has to be, an
# SMS gateway that Doze can silence is useless. But a whitelisted app is EXEMPT
# from the restrictions you are trying to test. Force Doze with the whitelist in
# place and everything keeps working, which tells you nothing about the phone of
# someone who never granted that exemption.
#
# Verified, not assumed: with the app whitelisted, `am set-standby-bucket rare`
# is silently ignored and the bucket stays at 5 (EXEMPTED). Remove the whitelist
# and the same command lands it at 40 (RARE).
#
# So `idle` takes the app OFF the whitelist, and that is the dangerous part: an
# SMS gateway left un-whitelisted goes quiet on the real install, and nothing
# announces it. `idle` therefore RECORDS the original state to ops/.doze-state
# and `normal` puts it back exactly. Always finish a session with `normal`.
#
# WHAT "3 HOURS" ACTUALLY MEANS
#
# Deep Doze begins ~30 min after screen-off + stationary + unplugged. Maintenance
# windows — the only moments a deferred job runs — then back off: 1h, 2h, 4h,
# then every 6h. Three hours in you are in deep Doze with windows hours apart and
# the app demoted to RARE. That is the state `idle` reproduces in one second.
#
# High-priority FCM still punches through Doze; normal-priority queues until the
# next maintenance window. If the api sends normal priority, that alone is a
# multi-hour delivery delay and it will show up here.
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ANDROID_DIR="$(dirname "$SCRIPT_DIR")"
STATE_FILE="$SCRIPT_DIR/.doze-state"

# ── adb ───────────────────────────────────────────────────────────────────────
#
# Not assumed to be on PATH. It usually is not in a non-login shell, and the
# failure ("adb: command not found") reads like a broken install rather than a
# PATH that simply never sourced a profile.

if command -v adb >/dev/null 2>&1; then
  ADB=adb
else
  SDK="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$HOME/Library/Android/sdk}}"
  if [ -x "$SDK/platform-tools/adb" ]; then
    ADB="$SDK/platform-tools/adb"
  else
    echo "❌ adb not found on PATH or under $SDK/platform-tools."
    echo "   export ANDROID_HOME=/path/to/sdk, or add platform-tools to PATH."
    exit 1
  fi
fi

# ── Device ────────────────────────────────────────────────────────────────────
#
# Exactly one, or adb picks for you and you spend ten minutes testing the wrong
# phone. ANDROID_SERIAL is adb's own variable, so honouring it costs nothing.

DEVICE_COUNT=$("$ADB" devices | awk 'NR>1 && $2=="device"' | wc -l | tr -d ' ')

if [ "$DEVICE_COUNT" -eq 0 ]; then
  echo "❌ No device. Check: $ADB devices"
  exit 1
fi

if [ "$DEVICE_COUNT" -gt 1 ] && [ -z "$ANDROID_SERIAL" ]; then
  echo "❌ $DEVICE_COUNT devices attached. Name one:"
  "$ADB" devices | awk 'NR>1 && $2=="device" {print "     export ANDROID_SERIAL=" $1}'
  exit 1
fi

# ── Package ───────────────────────────────────────────────────────────────────
#
# Read from build.gradle.kts rather than hardcoded, exactly like
# scripts/build-release.sh does, so renaming the app does not leave this script
# quietly testing a package that no longer exists.

APP_ID="${HTTPSMS_APP_ID:-$(sed -n -E 's/.*applicationId[[:space:]]*(=[[:space:]]*)?"([^"]*)".*/\2/p' \
         "$ANDROID_DIR/app/build.gradle.kts" | head -1)}"

if [ -z "$APP_ID" ]; then
  echo "❌ Could not read applicationId from app/build.gradle.kts"
  echo "   Override it: HTTPSMS_APP_ID=nl.example.sms $0 $*"
  exit 1
fi

# NOT CHECKED HERE, ON PURPOSE — see require_installed() below. `normal` must be
# able to run against a half-asleep phone, and gating it behind a package lookup
# is precisely what stops the whitelist ever coming back.

# ── Helpers ───────────────────────────────────────────────────────────────────

require_installed() {
  # DISTINGUISH "cannot reach the phone" FROM "app is missing". They need
  # different reactions and they look identical if you only grep for the package.
  #
  # This is not hypothetical. Deep Doze powers down wifi, so a device attached
  # over WIRELESS adb drops off mid-test — adb reconnects with a new
  # transport_id and, in the gap, `pm list packages` returns NOTHING. Grepping
  # that for the package reports "not installed" about an app that is installed,
  # and sent you to reinstall it.
  #
  # An empty list is never a real answer: a booted device always has hundreds of
  # packages. So empty means the connection, and only a non-empty list that
  # lacks the package means the app.
  _pkgs=$("$ADB" shell pm list packages 2>/dev/null | tr -d '\r')

  if [ -z "$_pkgs" ]; then
    echo "❌ Cannot reach the device (empty package list)."
    echo "   Deep Doze powers down wifi, so a WIRELESS adb connection drops"
    echo "   during exactly this test. Re-attach and try again:"
    echo "     $ADB devices        # is it still listed?"
    echo "     $ADB reconnect"
    echo ""
    echo "   Tip: run this over USB. A cable does not fall asleep."
    if [ -f "$STATE_FILE" ]; then
      echo ""
      echo "⚠️  A test IS in progress and the app is still off the battery"
      echo "    whitelist. Once the device is back: $0 normal"
    fi
    exit 1
  fi

  if ! printf '%s\n' "$_pkgs" | grep -q "^package:${APP_ID}$"; then
    echo "❌ $APP_ID is not installed on this device."
    echo "   ./scripts/build-release.sh --release && adb install -r ops/public/HttpSms.apk"
    exit 1
  fi
}

is_whitelisted() {
  # Lines look like: user,nl.hollandworx.sms,10201
  # Match on the commas so a package that is a prefix of another cannot match.
  "$ADB" shell dumpsys deviceidle whitelist 2>/dev/null | grep -q ",${APP_ID},"
}

bucket() { "$ADB" shell am get-standby-bucket "$APP_ID" 2>/dev/null | tr -d '\r'; }

bucket_name() {
  case "$1" in
    5)  echo "EXEMPTED (whitelisted — Doze does not apply)" ;;
    10) echo "ACTIVE" ;;
    20) echo "WORKING_SET" ;;
    30) echo "FREQUENT" ;;
    40) echo "RARE" ;;
    45) echo "RESTRICTED (worst case)" ;;
    50) echo "NEVER" ;;
    *)  echo "$1" ;;
  esac
}

# How many hours idle maps to which standby bucket.
#
# THESE ARE THE AOSP DEFAULTS, and they are slower than people expect:
# WORKING_SET after 2h unused, FREQUENT after 24h, RARE after 48h, RESTRICTED
# after weeks. So "3 hours" is WORKING_SET — the big effect at 3h is deep Doze,
# not the bucket.
#
# Vendor builds are another matter entirely. Samsung, Xiaomi and Huawei demote
# far harder and far sooner, which is why `--bucket` exists below: to reproduce a
# user's phone rather than the AOSP model of one.
bucket_for_hours() {
  h="$1"
  case "$h" in *.*) h="${h%%.*}" ;; esac   # 0.5 -> 0
  [ -z "$h" ] && h=0
  if   [ "$h" -lt 24 ]; then echo working_set
  elif [ "$h" -lt 48 ]; then echo frequent
  else                       echo rare
  fi
}

# ── Arguments ─────────────────────────────────────────────────────────────────
#
# `idle3` and `idle 3` mean the same thing. The glued form is what anyone
# actually types at a prompt, and refusing it over a missing space is the sort of
# thing that makes a tool annoying enough to stop using.

CMD="${1:-status}"
HOURS=""
FORCE_BUCKET=""

case "$CMD" in
  idle*|doze*)
    HOURS=$(printf '%s' "$CMD" | sed -n -E 's/^(idle|doze)([0-9]+)$/\2/p')
    CMD="idle"
    shift 2>/dev/null || true
    # A bare number after the command is the hour count: `idle 3`.
    case "${1:-}" in
      [0-9]*) [ -z "$HOURS" ] && HOURS="$1" && shift ;;
    esac
    while [ $# -gt 0 ]; do
      case "$1" in
        --bucket)       FORCE_BUCKET="${2:-}"; shift 2 ;;
        --no-allowlist) ALLOWLIST=remove; shift ;;
        --allowlist)    ALLOWLIST=keep; shift ;;
        *) echo "Unknown option: $1"; exit 1 ;;
      esac
    done
    [ -z "$HOURS" ] && HOURS=3
    ;;
esac

# DEFAULT IS "keep": put the app ON the battery allowlist before dozing.
#
# That is what this deployment actually ships — the app is useless without the
# exemption and users are told to grant it — so it is the configuration worth
# testing by default. `idle3` answers "does MY setup survive Doze".
#
# --no-allowlist answers the other question, which is also worth asking but is
# not the same one: "what happens to a user who never granted the exemption".
# Expect that one to fail on normal-priority FCM; see the api's
# phone_notification_service.go.
ALLOWLIST="${ALLOWLIST:-keep}"

# ── Commands ──────────────────────────────────────────────────────────────────

case "$CMD" in

  status|-s)
    echo "📱 $APP_ID"
    echo ""
    echo "  deep doze : $("$ADB" shell dumpsys deviceidle get deep | tr -d '\r')"
    echo "  light doze: $("$ADB" shell dumpsys deviceidle get light | tr -d '\r')"
    B=$(bucket)
    echo "  bucket    : $B  $(bucket_name "$B")"
    if is_whitelisted; then
      echo "  allowlist : YES — the shipped config (what \`idle3\` tests)"
    else
      echo "  allowlist : no  — un-exempt user (what \`--no-allowlist\` tests)"
    fi
    if [ -f "$STATE_FILE" ]; then
      echo ""
      echo "⚠️  A test is IN PROGRESS (ops/.doze-state exists)."
      echo "    The device is not in its normal state. Finish with: $0 normal"
    fi
    ;;

  idle)
    require_installed
    TARGET_BUCKET="${FORCE_BUCKET:-$(bucket_for_hours "$HOURS")}"

    # SAVE FIRST, AND ONLY ONCE. Running `idle` twice must not overwrite the
    # state file with the already-modified state — that is how the original
    # whitelist entry gets lost and the real install goes quiet for good.
    if [ -f "$STATE_FILE" ]; then
      echo "ℹ️  ops/.doze-state already exists; keeping the ORIGINAL saved state."
    else
      if is_whitelisted; then WAS_WL=yes; else WAS_WL=no; fi
      printf 'WAS_WHITELISTED=%s\nORIG_BUCKET=%s\n' "$WAS_WL" "$(bucket)" > "$STATE_FILE"
      echo "💾 Saved current state to ops/.doze-state"
    fi

    echo ""
    if [ "$ALLOWLIST" = "keep" ]; then
      echo "😴 Simulating ${HOURS}h idle for $APP_ID"
      echo "   deep Doze, app ON the battery allowlist — the SHIPPED config"
    else
      echo "😴 Simulating ${HOURS}h idle for $APP_ID"
      echo "   deep Doze, app OFF the allowlist + bucket ${TARGET_BUCKET}"
      echo "   (harsher than production: a user who never granted the exemption)"
    fi
    echo ""

    if [ "$ALLOWLIST" = "keep" ]; then
      # ADD IT, do not merely leave it. The app may have been dropped by an
      # earlier --no-allowlist run in this same session, and silently testing
      # the wrong configuration is the failure this whole script exists to stop.
      "$ADB" shell dumpsys deviceidle whitelist "+$APP_ID" >/dev/null 2>&1 || true
      # No bucket call here on purpose: an allowlisted app is pinned to EXEMPTED
      # by the system, so setting one would be a no-op that reads like it worked.
    else
      "$ADB" shell dumpsys deviceidle whitelist "-$APP_ID" >/dev/null 2>&1 || true
      "$ADB" shell am set-standby-bucket "$APP_ID" "$TARGET_BUCKET" >/dev/null 2>&1 || true
    fi

    # Order matters. Doze will not engage while the device believes it is
    # charging or the screen is on, and force-idle just refuses.
    "$ADB" shell dumpsys battery unplug >/dev/null 2>&1
    "$ADB" shell input keyevent KEYCODE_SLEEP >/dev/null 2>&1
    "$ADB" shell dumpsys deviceidle force-idle >/dev/null 2>&1

    B=$(bucket)
    echo "  deep doze : $("$ADB" shell dumpsys deviceidle get deep | tr -d '\r')"
    echo "  bucket    : $B  $(bucket_name "$B")"
    if is_whitelisted; then
      echo "  allowlist : YES — as shipped"
    else
      echo "  allowlist : no  — un-exempt user"
    fi
    echo ""

    if [ "$ALLOWLIST" = "keep" ] && ! is_whitelisted; then
      echo "⚠️  Asked to keep the allowlist but the app is NOT on it. The test"
      echo "    below is harsher than production and a failure may not be real."
      echo ""
    fi

    if [ "$ALLOWLIST" = "remove" ] && [ "$B" = "5" ]; then
      echo "⚠️  Bucket is still EXEMPTED — something re-added the app to the"
      echo "    allowlist, so Doze restrictions are NOT in force and this test"
      echo "    will pass no matter how broken the app is."
      echo ""
    fi

    echo "▶ Now send a message through the api and watch it arrive (or not):"
    echo "    $0 logs"
    echo ""
    if [ "$ALLOWLIST" = "keep" ]; then
      echo "🛑 WHEN DONE — the device is still in forced Doze and believes it is"
      echo "   unplugged until you run:"
      echo "     $0 normal"
    else
      echo "🛑 WHEN DONE — do not skip this. The app stays OFF the battery"
      echo "   allowlist until you run it, and stays quiet on the real install:"
      echo "     $0 normal"
    fi
    ;;

  normal|wake|reset)
    echo "☀️  Restoring..."

    # WAKE IT FIRST, THEN WAIT. A phone in deep Doze on wireless adb may have
    # dropped off; waking it brings the radio back. Everything below is best
    # effort (|| true) for the same reason — a single unreachable command must
    # not abort the run and strand the app off the whitelist, which is what
    # `set -e` would otherwise do.
    "$ADB" reconnect >/dev/null 2>&1 || true
    "$ADB" wait-for-device >/dev/null 2>&1 || true

    "$ADB" shell dumpsys deviceidle unforce >/dev/null 2>&1 || true
    "$ADB" shell dumpsys battery reset >/dev/null 2>&1 || true
    "$ADB" shell input keyevent KEYCODE_WAKEUP >/dev/null 2>&1 || true

    if [ -f "$STATE_FILE" ]; then
      # shellcheck disable=SC1090
      . "$STATE_FILE"

      RESTORE_OK=yes

      if [ "${WAS_WHITELISTED:-no}" = "yes" ]; then
        "$ADB" shell dumpsys deviceidle whitelist "+$APP_ID" >/dev/null 2>&1 || true
        # VERIFY, do not assume. This is the whole safety property of the script:
        # the app was taken off the battery whitelist and an SMS gateway that
        # stays off it goes quiet with nothing to show for it. A restore that
        # silently failed is worse than one that never ran, because the state
        # file would be deleted and the original setting lost.
        if is_whitelisted; then
          echo "  ✓ battery whitelist restored"
        else
          RESTORE_OK=no
          echo "  ❌ WHITELIST NOT RESTORED — the app is still exempt-less."
          echo "     Keeping ops/.doze-state so this can be retried."
          echo "     Retry: $0 normal"
          echo "     Or by hand:"
          echo "       $ADB shell dumpsys deviceidle whitelist +$APP_ID"
        fi
      else
        echo "  ✓ was not whitelisted before; left off"
      fi

      # Set the bucket AFTER the whitelist. A whitelisted app is pinned to
      # EXEMPTED by the system, so this is a no-op in that case — which is
      # correct, and the reason it is not worth special-casing.
      "$ADB" shell am set-standby-bucket "$APP_ID" "${ORIG_BUCKET:-10}" >/dev/null 2>&1 || \
        "$ADB" shell am set-standby-bucket "$APP_ID" active >/dev/null 2>&1 || true

      if [ "$RESTORE_OK" = "yes" ]; then
        rm -f "$STATE_FILE"
        echo "  ✓ ops/.doze-state cleared"
      fi
    else
      echo "  ℹ️  No ops/.doze-state — nothing was saved, so the whitelist is"
      echo "     left exactly as it is. Check it below."
    fi

    echo ""
    "$0" status
    ;;

  step)
    # The staged walk: ACTIVE -> INACTIVE -> IDLE_PENDING -> SENSING -> LOCATING
    # -> IDLE. Closer to a real device than force-idle, because SENSING and
    # LOCATING are exactly where motion on a real phone kicks it back out.
    "$ADB" shell dumpsys deviceidle step deep | tr -d '\r'
    ;;

  logs|-l)
    require_installed
    echo "📜 Following $APP_ID + FCM. Ctrl-C to stop."
    "$ADB" logcat -c 2>/dev/null || true
    exec "$ADB" logcat -s httpsms:V FirebaseMessaging:V "${APP_ID}:V" '*:E'
    ;;

  *)
    echo "Unknown command: $1"
    sed -n '3,10p' "$0" | sed 's/^# \{0,1\}//'
    exit 1
    ;;
esac
