package com.localghost.app.ui

data class SyncUiState(
    val hasImages: Boolean = false,
    val hasVideo: Boolean = false,
    val hasLocation: Boolean = false,
    val partial: Boolean = false,
    val status: String? = null,
    val isError: Boolean = false,
    val busy: Boolean = false,
    val notificationsMuted: Boolean = false,
    val photoTotal: Int = 0,
    val photoDone: Int = 0,
    val videoTotal: Int = 0,
    val videoDone: Int = 0,
    val curName: String = "",
    val curVideoName: String = "",
    val curVideoRead: Long = 0,
    val curVideoSize: Long = 0,
    // Throughput + ETA. bytesSent is the running total this run; bytesTotal is the estimated total to
    // send (sum of item sizes). speedBps is a smoothed bytes/sec. etaSeconds is derived from the two.
    val bytesSent: Long = 0,
    val bytesTotal: Long = 0,
    val speedBps: Double = 0.0,
    val etaSeconds: Long = 0,
)
