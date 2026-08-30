// CUSTOM: emptied for self-hosting. Upstream shipped two third-party
// integrations here, both wired to the UPSTREAM PROJECT'S OWN accounts:
//
//   1. Microsoft Clarity, project id 'f3xyl9wf6t' — session RECORDING, not just
//      page counts. It captures clicks, scroll and DOM snapshots and posts them
//      to https://t.clarity.ms/collect. On a self-hosted install that means the
//      contents of this console — contacts, thread lists, phone numbers on
//      screen — were being recorded and delivered to someone else's analytics
//      dashboard, with a 204 back and nothing visible in the UI.
//
//   2. window.lemonSqueezyAffiliateConfig = { store: 'httpsms' } — upstream's
//      billing/affiliate store. Irrelevant here; ENTITLEMENT_ENABLED is false
//      and there is no checkout.
//
// The file itself is KEPT rather than deleted: nuxt.config.ts references
// /integrations.js, so removing it would turn every page load into a 404.
//
// If you ever want your OWN analytics, add it here with YOUR id — and prefer
// something that does not record sessions.
