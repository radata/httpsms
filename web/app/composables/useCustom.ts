// CUSTOM FILE — not upstream.
//
// Local behaviour changes live here so that pulling a new httpSMS release
// touches upstream .vue files as little as possible. Vue single-file components
// cannot be split the way Go files can (there is no partial-override mechanism
// for a template), so the rule for this repo is:
//
//   * all LOGIC and every default value lives in this file
//   * the upstream component keeps a one-line call, marked `CUSTOM:`
//
// That way `git diff` against a fresh upstream checkout shows a handful of
// single-line hunks instead of rewritten components, and the reasoning is in
// one place instead of scattered through the page tree.
//
// Every helper here is PURE — it takes a value and returns a value, and never
// calls useRuntimeConfig() itself. That matters: several call sites are event
// handlers that run outside Vue's setup context, where Nuxt composables are not
// guaranteed to resolve. The caller reads config at setup scope and passes the
// value in.

/**
 * Whether the realtime layer should be wired up at all.
 *
 * PUSHER_KEY is empty in a self-hosted install, and pusher-js does NOT throw on
 * an empty key — it accepts it, then retries a websocket against a malformed
 * host forever (the endless openStream/tryStrategy frames in the console).
 *
 * Worse, in the default layout Pusher's `phone.updated` event was the ONLY
 * thing that ever set `canPoll`, and `canPoll` gates the whole polling loop.
 * With no key the dashboard therefore got neither push NOR polling and never
 * refreshed after first load. Upstream never hit this because httpsms.com
 * always has Pusher configured.
 */
export function pusherEnabledCustom(pusherKey: unknown): boolean {
  return typeof pusherKey === 'string' && pusherKey.trim() !== ''
}

/**
 * Default ISO-3166 alpha-2 country for phone-number entry.
 *
 * Upstream hardcodes 'US' in the contact dialog and the send-message form, so
 * a Dutch number typed without a +31 prefix is parsed as American and silently
 * stored wrong. Driven by DEFAULT_COUNTRY instead.
 *
 * Falls back to 'US' — upstream's value — when unset or malformed, so an
 * unconfigured deployment behaves exactly as before rather than picking a new
 * surprise default.
 */
export function defaultCountryCustom(value: unknown): string {
  const country = String(value ?? '')
    .trim()
    .toUpperCase()
  return /^[A-Z]{2}$/.test(country) ? country : 'US'
}

/**
 * Whether this install sells plans at all.
 *
 * Upstream's billing page always shows `<used>/200 messages`, an Upgrade Plan
 * button and the Pro/Enterprise cards, because httpsms.com always has a Lemon
 * Squeezy store behind it. A self-hosted install that charges nobody has
 * neither, and showing them is worse than noise: the checkout URLs shipped in
 * .env.production point at NdoleStudio's OWN store, so a user who clicks
 * Upgrade pays upstream for a plan this server will never see.
 *
 * Parsed exactly like the api parses ENTITLEMENT_ENABLED (`== "true"`, see
 * pkg/di/container.go), so the same value in both .env files means the same
 * thing on both sides. Anything else — unset, empty, "1", the unsubstituted
 * placeholder — is off, which is the safe direction: no ceiling is claimed that
 * the api would not enforce.
 */
export function entitlementEnabledCustom(value: unknown): boolean {
  return String(value ?? '').trim() === 'true'
}
