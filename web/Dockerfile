# build stage
FROM node:22-alpine AS build

WORKDIR /app

# pnpm-workspace.yaml IS REQUIRED HERE, not just in the `COPY . .` below.
# It carries no `packages:` — its only content is pnpm 10's `allowBuilds:` list
# (esbuild, @parcel/watcher, protobufjs, @firebase/util, unrs-resolver,
# vue-demi). pnpm 10 refuses to run dependency build scripts unless they are
# approved there, and reports the refusal as the WARNING ERR_PNPM_IGNORED_BUILDS
# rather than an error. Omit this file at install time and those packages
# silently never place their platform binaries; the build then fails much later,
# inside `nuxt generate`, with an error that looks unrelated to dependencies.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./

# package.json's `prepare` script is `cd .. && husky web/.husky`, and husky
# exits 1 with ".git can't be found" when there is no repo — which there never
# is inside a build context. HUSKY=0 makes husky skip and exit 0 instead.
# Git hooks are a developer-machine concern; they have no meaning in an image.
ENV HUSKY=0

# PIN THE MAJOR. The bare `pnpm` tag moves, and this project is built for 10:
# `allowBuilds:` is a pnpm 10 key, and pnpm 9 rejects a pnpm-workspace.yaml that
# has no `packages:` field with "packages field missing or empty".
RUN npm install -g pnpm@10

RUN pnpm install

COPY . .

# BUILD WITH PLACEHOLDERS — THE IMAGE CARRIES NO CONFIGURATION.
#
# `nuxt generate` inlines runtimeConfig.public into the JS payload, so upstream's
# web is configured at BUILD time. That would make the dev image and the prod
# image different artifacts, put the web outside the rule every other service
# here follows (one build, configured by the server's own .env), and hide a wrong
# value until someone opens a browser after a deploy.
#
# So we generate with __HTTPSMS_<VAR>__ markers and substitute them at CONTAINER
# START from the environment — see ops/runtime-config.sh, installed into
# /docker-entrypoint.d below. One image runs in dev and in production, exactly
# like the Go api.
#
# This .env is written HERE rather than copied from the build context on purpose:
# nothing about the host can leak into the image, a symlinked web/.env can no
# longer arrive dangling and silently bake upstream's api.httpsms.com, and there
# is no build-time secret to keep out of a layer.
#
# KEEP IN SYNC with VARS in ops/runtime-config.sh. A name here with no
# counterpart there is never substituted and ships the literal placeholder to the
# browser; the script asserts the two critical ones actually landed.
RUN printf '%s\n' \
      'API_BASE_URL=__HTTPSMS_API_BASE_URL__' \
      'APP_URL=__HTTPSMS_APP_URL__' \
      'APP_NAME=__HTTPSMS_APP_NAME__' \
      'APP_ENV=__HTTPSMS_APP_ENV__' \
      'APP_GITHUB_URL=__HTTPSMS_APP_GITHUB_URL__' \
      'APP_DOCUMENTATION_URL=__HTTPSMS_APP_DOCUMENTATION_URL__' \
      'APP_DOWNLOAD_URL=__HTTPSMS_APP_DOWNLOAD_URL__' \
      'CHECKOUT_URL=__HTTPSMS_CHECKOUT_URL__' \
      'ENTERPRISE_CHECKOUT_URL=__HTTPSMS_ENTERPRISE_CHECKOUT_URL__' \
      'CLOUDFLARE_TURNSTILE_SITE_KEY=__HTTPSMS_CLOUDFLARE_TURNSTILE_SITE_KEY__' \
      'PUSHER_KEY=__HTTPSMS_PUSHER_KEY__' \
      'PUSHER_CLUSTER=__HTTPSMS_PUSHER_CLUSTER__' \
      'DEFAULT_COUNTRY=__HTTPSMS_DEFAULT_COUNTRY__' \
      'ENTITLEMENT_ENABLED=__HTTPSMS_ENTITLEMENT_ENABLED__' \
      'FIREBASE_API_KEY=__HTTPSMS_FIREBASE_API_KEY__' \
      'FIREBASE_AUTH_DOMAIN=__HTTPSMS_FIREBASE_AUTH_DOMAIN__' \
      'FIREBASE_PROJECT_ID=__HTTPSMS_FIREBASE_PROJECT_ID__' \
      'FIREBASE_STORAGE_BUCKET=__HTTPSMS_FIREBASE_STORAGE_BUCKET__' \
      'FIREBASE_MESSAGING_SENDER_ID=__HTTPSMS_FIREBASE_MESSAGING_SENDER_ID__' \
      'FIREBASE_APP_ID=__HTTPSMS_FIREBASE_APP_ID__' \
      'FIREBASE_MEASUREMENT_ID=__HTTPSMS_FIREBASE_MEASUREMENT_ID__' \
      'GITHUB_SHA=__HTTPSMS_GITHUB_SHA__' \
    > .env

RUN pnpm run generate

# production stage
FROM nginx:stable-alpine AS production
COPY --from=build /app/.output/public /usr/share/nginx/html

# Copy the nginx configuration file
COPY ./nginx.conf /etc/nginx/conf.d/default.conf

# Runs before nginx starts — the nginx image executes /docker-entrypoint.d/*.sh
# in name order, so 40- lands after its own 10/15/20/30 scripts.
COPY ./ops/runtime-config.sh /docker-entrypoint.d/40-httpsms-runtime-config.sh
RUN chmod +x /docker-entrypoint.d/40-httpsms-runtime-config.sh

EXPOSE 3000
CMD ["nginx", "-g", "daemon off;"]
