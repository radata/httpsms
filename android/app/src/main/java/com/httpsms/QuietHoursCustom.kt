package com.httpsms

import android.content.Context
import androidx.preference.PreferenceManager
import androidx.work.Constraints
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequest
import androidx.work.WorkManager
import com.httpsms.worker.HeartbeatWorker
import okhttp3.OkHttpClient
import okhttp3.Request
import org.json.JSONObject
import timber.log.Timber
import java.net.URLEncoder
import java.time.ZoneId
import java.time.ZonedDateTime
import java.util.concurrent.TimeUnit

// CUSTOM FILE — not upstream. Quiet hours, app side.
//
// THE AIM: during quiet hours httpSMS causes NO network traffic. Every request
// wakes the data radio, and on this phone a radio wake brings the VPN up and
// fires the connectivity event a pile of other apps react to. The app is not
// killed — it stays installed and running, it just keeps quiet.
//
// Quiet hours are the hours OUTSIDE the send windows of the message send
// schedule attached to the phone on the server (web → Settings → phone). The
// server holds outgoing messages and its own heartbeat pushes for exactly those
// hours (api/pkg/services/quiet_hours_custom.go). This file makes the app match:
//
//   - HeartbeatWorker skips its network call while quiet and instead books one
//     check-in for the moment the window opens (deferHeartbeat).
//   - Received SMS, missed calls and delivery reports are still captured as they
//     happen, but their upload waits for the window (quietDelayCustom). Nothing
//     is dropped: WorkManager persists the work across reboots.
//   - After every successful heartbeat the app re-reads the windows from
//     GET /v1/phones/quiet-hours (refresh), so a schedule edited on the server
//     reaches the phone on its next heartbeat inside a window.
//
// No cached schedule, or one with no windows, means never quiet — upstream
// behaviour.
object QuietHoursCustom {
    private const val PREF_PREFIX = "quiet_hours_custom_"
    private const val CHECK_IN_WORK = "quiet_hours_custom_check_in"

    /** Re-reads each active SIM's windows from the server. Call only while the network is in use anyway. */
    fun refresh(context: Context) {
        val client = OkHttpClient()
        val baseURL = Settings.getServerUrlOrDefault(context)
        for (owner in activeOwners(context)) {
            try {
                val path = baseURL.path + "/v1/phones/quiet-hours?owner=" + URLEncoder.encode(owner, "UTF-8")
                val request = Request.Builder()
                    .url(baseURL.resolve(path).toURL())
                    .header("x-api-key", Settings.getApiKeyOrDefault(context))
                    .header("X-Client-Version", BuildConfig.VERSION_NAME)
                    .build()
                client.newCall(request).execute().use { response ->
                    if (!response.isSuccessful) {
                        Timber.w("cannot fetch quiet hours for [$owner]: HTTP ${response.code}")
                        return@use
                    }
                    val data = JSONObject(response.body!!.string()).getJSONObject("data")
                    PreferenceManager.getDefaultSharedPreferences(context).edit()
                        .putString(PREF_PREFIX + owner, data.toString())
                        .apply()
                    Timber.d("quiet hours for [$owner] refreshed")
                }
            } catch (exception: Exception) {
                Timber.w(exception, "cannot refresh quiet hours for [$owner]")
            }
        }
    }

    /**
     * True when every active SIM is quiet. The caller must then skip its
     * heartbeat; one check-in has been booked for when the first window opens.
     */
    fun deferHeartbeat(context: Context): Boolean {
        val delay = uploadDelayMillis(context, null)
        if (delay <= 0) {
            return false
        }

        val work = OneTimeWorkRequest.Builder(HeartbeatWorker::class.java)
            .setInitialDelay(delay, TimeUnit.MILLISECONDS)
            .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
            .build()
        WorkManager.getInstance(context).enqueueUniqueWork(CHECK_IN_WORK, ExistingWorkPolicy.REPLACE, work)
        Timber.i("quiet hours: heartbeat skipped, check-in booked in [${delay / 60000}] minutes")
        return true
    }

    /**
     * Milliseconds until [owner]'s window opens; 0 when it is open now. With a
     * null owner it answers for the whole phone: 0 as soon as any active SIM is
     * open, otherwise the soonest opening.
     */
    fun uploadDelayMillis(context: Context, owner: String?, now: ZonedDateTime = ZonedDateTime.now()): Long {
        val owners = if (owner != null) listOf(owner) else activeOwners(context)
        if (owners.isEmpty()) {
            return 0
        }
        return owners.minOf { millisUntilOpen(cached(context, it), now) }
    }

    private fun activeOwners(context: Context): List<String> {
        val owners = mutableListOf<String>()
        if (Settings.getActiveStatus(context, Constants.SIM1)) {
            owners.add(Settings.getSIM1PhoneNumber(context))
        }
        if (Settings.getActiveStatus(context, Constants.SIM2)) {
            owners.add(Settings.getSIM2PhoneNumber(context))
        }
        return owners
    }

    private fun cached(context: Context, owner: String): JSONObject? {
        val raw = PreferenceManager.getDefaultSharedPreferences(context).getString(PREF_PREFIX + owner, null)
            ?: return null
        return try {
            JSONObject(raw)
        } catch (exception: Exception) {
            null
        }
    }

    /**
     * Same arithmetic as MessageSendSchedule.ResolveScheduledAt on the server,
     * so both ends agree on when the window opens. Go numbers weekdays
     * Sunday=0..Saturday=6; java.time is Monday=1..Sunday=7, hence `% 7`.
     */
    internal fun millisUntilOpen(schedule: JSONObject?, now: ZonedDateTime): Long {
        if (schedule == null) {
            return 0
        }
        val windows = schedule.optJSONArray("windows") ?: return 0
        if (windows.length() == 0) {
            return 0
        }
        val zone = try {
            ZoneId.of(schedule.optString("timezone"))
        } catch (exception: Exception) {
            return 0
        }

        val base = now.withZoneSameInstant(zone)
        var best: ZonedDateTime? = null
        for (dayOffset in 0..7) {
            val midnight = base.toLocalDate().plusDays(dayOffset.toLong()).atStartOfDay(zone)
            val weekday = midnight.dayOfWeek.value % 7
            for (i in 0 until windows.length()) {
                val window = windows.getJSONObject(i)
                if (window.optInt("day_of_week", -1) != weekday) {
                    continue
                }
                val start = midnight.plusMinutes(window.optLong("start_minute"))
                val end = midnight.plusMinutes(window.optLong("end_minute"))
                val candidate = when {
                    dayOffset == 0 && base.isBefore(start) -> start
                    dayOffset == 0 && !base.isBefore(start) && base.isBefore(end) -> return 0
                    dayOffset > 0 -> start
                    else -> null
                }
                if (candidate != null && (best == null || candidate.isBefore(best))) {
                    best = candidate
                }
            }
            if (best != null) {
                break
            }
        }
        return best?.let { java.time.Duration.between(base, it).toMillis().coerceAtLeast(0) } ?: 0
    }
}

/** Hook for upstream work builders: hold an upload until the quiet period ends. */
fun OneTimeWorkRequest.Builder.quietDelayCustom(context: Context, owner: String?): OneTimeWorkRequest.Builder =
    setInitialDelay(QuietHoursCustom.uploadDelayMillis(context, owner), TimeUnit.MILLISECONDS)
