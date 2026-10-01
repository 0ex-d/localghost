package com.localghost.app.notify

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import com.localghost.app.net.BoxClient
import java.util.concurrent.TimeUnit

/** 15-min notification poll. Tiny payloads, so no network constraint. */
class PollWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {
    override suspend fun doWork(): Result {
        Notifications.ensureChannel(applicationContext)
        // voice notes made while the box was out of reach go now (a few MB each; kept until it has them)
        try { com.localghost.app.voice.VoiceNotes.uploadPending(applicationContext) } catch (_: Exception) {}
        // the box hears what network this phone is on: on Wi-Fi the phone fetches the feeds and
        // the tickers for it, on mobile data or in silence the box fetches for itself
        try { BoxClient.reportNet(applicationContext) } catch (_: Exception) {}
        return try {
            Notifications.postBatch(applicationContext, BoxClient.pollPending(applicationContext))
            Result.success()
        } catch (e: Exception) { Result.retry() }
    }
    companion object {
        private const val NAME = "localghost.poll"
        fun schedule(ctx: Context) {
            WorkManager.getInstance(ctx).enqueueUniquePeriodicWork(
                NAME, ExistingPeriodicWorkPolicy.KEEP,
                PeriodicWorkRequestBuilder<PollWorker>(15, TimeUnit.MINUTES).build())
        }
    }
}
