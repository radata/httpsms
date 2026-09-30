<script setup lang="ts">
// CUSTOM FILE — not upstream. Served at /app-access.
//
// Upstream's "Download App" links point at its own APK, which is built against
// upstream's Firebase project and cannot sign in to this server. Our build is a
// Google Play test app, and Play only admits testers whose Google account the
// operator has added in the Play Console. So every download link in the app and
// blog now points here instead, and this form emails the request to the
// operator (api: handlers/app_access_handler_custom.go).
//
// Signed-in only: the `auth` middleware sends a visitor to /login?to=/app-access
// and back, and the request carries their account email for free.
import { mdiArrowLeft, mdiCheckCircleOutline, mdiSend } from '@mdi/js'
import { ErrorMessages } from '~/utils/errors'
import { getApiErrorMessage, toApiError } from '~/utils/api-error'

definePageMeta({
  path: '/app-access',
  middleware: ['auth'],
})

useHead({
  title: 'Get the Android App - httpSMS',
})

const authStore = useAuthStore()
const notificationsStore = useNotificationsStore()
const { apiFetch } = useApi()

const form = ref({
  name: authStore.authUser?.displayName ?? '',
  google_play_email: authStore.authUser?.email ?? '',
  note: '',
})
const sending = ref(false)
const sent = ref(false)
const errorMessages = ref(new ErrorMessages())

async function submit() {
  sending.value = true
  errorMessages.value = new ErrorMessages()
  try {
    await apiFetch('/v1/app-access-requests', {
      method: 'POST',
      body: form.value,
    })
    sent.value = true
  } catch (error) {
    const bag = new ErrorMessages()
    const data = toApiError(error).data?.data
    if (data && typeof data === 'object') {
      Object.keys(data).forEach((key) => bag.addMany(key, data[key]))
    }
    errorMessages.value = bag
    notificationsStore.addNotification({
      type: 'error',
      message: getApiErrorMessage(error, 'Your request could not be sent'),
    })
  } finally {
    sending.value = false
  }
}
</script>

<template>
  <VContainer fluid class="px-0 pt-0">
    <VAppBar>
      <VBtn icon to="/threads">
        <VIcon :icon="mdiArrowLeft" />
      </VBtn>
      <VToolbarTitle>Get the Android App</VToolbarTitle>
    </VAppBar>
    <VContainer class="pt-0">
      <VRow>
        <VCol cols="12" md="8" offset-md="2" xl="6" offset-xl="3">
          <h5 class="text-md-display-small text-title-large mt-3 mb-4">
            Get the Android App
          </h5>

          <VAlert
            v-if="sent"
            type="success"
            variant="tonal"
            :icon="mdiCheckCircleOutline"
          >
            <p class="text-body-large mb-2">Your request has been sent.</p>
            <p class="text-body-medium mb-0">
              Once approved, Google Play emails an invitation to
              <b>{{ form.google_play_email }}</b>. Open it on your Android phone
              to install the app, then sign in with your API key.
            </p>
          </VAlert>

          <template v-else>
            <p class="text-body-large">
              The httpSMS Android app is in testing on Google Play. Request
              access below, and we will add your Google account as a tester.
            </p>
            <VForm class="mt-4" @submit.prevent="submit">
              <VTextField
                v-model="form.name"
                label="Your name"
                variant="outlined"
                :error="errorMessages.has('name')"
                :error-messages="errorMessages.get('name')"
                :disabled="sending"
              />
              <VTextField
                v-model="form.google_play_email"
                label="Google account email"
                type="email"
                hint="The Gmail address signed in to Google Play on your Android phone"
                persistent-hint
                variant="outlined"
                class="mt-2"
                :error="errorMessages.has('google_play_email')"
                :error-messages="errorMessages.get('google_play_email')"
                :disabled="sending"
              />
              <VTextarea
                v-model="form.note"
                label="Note (optional)"
                variant="outlined"
                rows="3"
                auto-grow
                class="mt-4"
                :error="errorMessages.has('note')"
                :error-messages="errorMessages.get('note')"
                :disabled="sending"
              />
              <VBtn
                type="submit"
                color="primary"
                size="large"
                :loading="sending"
                :prepend-icon="mdiSend"
              >
                Request access
              </VBtn>
            </VForm>
          </template>
        </VCol>
      </VRow>
    </VContainer>
  </VContainer>
</template>
