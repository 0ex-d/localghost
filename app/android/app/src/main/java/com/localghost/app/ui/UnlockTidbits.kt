package com.localghost.app.ui

import com.localghost.app.net.ModelLoad
import com.localghost.app.net.UnlockStage

/**
 * The lines under the unlock bar: what the box is doing right now, in plain words, turn about with
 * something the app can do that is easy to miss. Every tip names a real place in the app. Pure (no
 * Compose), so the words are tested; the screen picks one per [line] tick.
 */
object UnlockTidbits {
    /** What the box is doing in [stage]; for MODEL, the load's own phase and percent. */
    fun doing(stage: UnlockStage?, model: ModelLoad?): String = when (stage) {
        null -> "open"
        UnlockStage.RESOLVE -> "checking the PIN on the box"
        UnlockStage.UNSEAL -> "unsealing the volume key , it never leaves the box"
        UnlockStage.MOUNT -> "opening the encrypted store"
        UnlockStage.START_DB -> "Postgres wakes up inside the encrypted store"
        UnlockStage.START_CACHE -> "starting the box's services: photos, search, memories, voice"
        UnlockStage.DAEMONS -> "the services check in"
        UnlockStage.MODEL -> when (model?.phase) {
            "weights" -> "loading the model's weights into the GPU" + pct(model)
            "projector" -> "loading the part of the model that sees photos"
            "warmup", "finishing" -> "one first word to warm the model up"
            "failed" -> "the model did not load , the box tries again by itself"
            "ready" -> "the model is ready"
            else -> "starting the model engine"
        }
        UnlockStage.READY -> "opening the doors"
        UnlockStage.STOP_SERVICES -> "stopping the services"
        UnlockStage.STOP_CACHE -> "flushing the cache"
        UnlockStage.STOP_DB -> "Postgres writes its last checkpoint"
        UnlockStage.UNMOUNT -> "closing the encrypted store; the key leaves memory"
        UnlockStage.LOCKED -> "locked"
    }

    private fun pct(m: ModelLoad): String = if (m.pct in 1..99) ": ${m.pct}%" else ""

    /**
     * Why a step takes the time it does, in one plain clause, for the steps where the wait is real
     * work rather than a formality. Shown under [doing] so a step that sits for a second or two
     * reads as "this is doing something", not "this is stuck". "" for the quick steps.
     */
    fun why(stage: UnlockStage?): String = when (stage) {
        UnlockStage.RESOLVE -> "the box checks the PIN in its secure chip, which answers at its own pace so guesses cannot be rushed"
        UnlockStage.UNSEAL -> "the key is unwrapped from the secure chip, the one step that turns your PIN into the key to the store"
        UnlockStage.MOUNT -> "the encrypted store is opened and checked before anything reads from it"
        UnlockStage.START_DB -> "Postgres opens its files and replays anything it had not finished writing"
        UnlockStage.START_CACHE -> "each service comes up in turn: photos, search, memories, voice"
        UnlockStage.DAEMONS -> "the box waits for every service to report it is ready"
        UnlockStage.STOP_DB -> "Postgres writes its last checkpoint so nothing is lost before the store closes"
        UnlockStage.UNMOUNT -> "the store is closed and the key wiped from memory"
        else -> ""
    }

    /** Things the app does that are easy to miss. Each names where to find it. */
    val tips: List<String> = listOf(
        "record a voice note on the daily check-in in MEMORIES; the box writes it down for you",
        "‹ and › on the MAP step through your days; \"all days\" lays them all at once",
        "ON THIS DAY in MEMORIES shows what you were doing on this date in other years",
        "NEAR YOU in MEMORIES finds the places you have been close to where you are now",
        "the gallery searches by place and tag: try \"waterfall vancouver island\"",
        "chat can search the web (DuckDuckGo, or Brave with your own key, which stays on the phone)",
        "MODELS puts a small model on the phone that answers when the box is out of reach",
        "PHRASES puts a line of the local language on your lock screen when you travel",
        "\"+ jot a note\" in MEMORIES sends a thought to the journal; the box decides what to keep",
        "WHAT YOU PHOTOGRAPH in MEMORIES is what your camera keeps coming back to",
        "SETTINGS › LOCK BOX NOW closes the vault from anywhere; the key leaves the box's memory",
        "BOX STATUS shows every service on the box and what it is working on",
        "HEALTH keeps a month of the phone's health numbers on the box, drawn as plain bars",
        "the hat-and-glasses icon at the top of chat is incognito: that chat is kept nowhere, not on the phone, not on the box",
        "the globe at the top of chat is web search; on auto the box decides when a question needs the web",
        "close the app while the box is answering and it keeps writing; the whole answer is there when you come back",
        "a follow-up like \"and tomorrow?\" carries the question before it, on the box and on the phone model",
        "an answer keeps its thinking and the pages it read; they are saved with the chat on the box",
        "SETTINGS › MAPS ON THIS PHONE downloads the streets around your places on Wi-Fi, so the MAP opens fast",
        "MODELS › benchmark shows how fast this phone reads and writes with its model",
        "the phone model stays loaded once you pick it in chat, so the second answer starts at once",
        "tap a photo in MEMORIES to open it; ON THIS DAY plays its photos as a slideshow",
        "the box never talks to the internet on its own; a web search leaves from the phone, not the box",
        "the box loads its model after the unlock; if you ask chat before it is ready, chat shows the load and answers once it is",
    )

    /**
     * The line for [tick] (the screen ticks every few seconds): even ticks say what is happening,
     * odd ones a tip. [seed] shuffles where the tips start, so each unlock does not open on the
     * same one.
     */
    fun line(tick: Int, stage: UnlockStage?, model: ModelLoad?, seed: Int): String =
        if (tick % 2 == 0) "> " + doing(stage, model) else "did you know? " + tip(tick / 2, seed)

    /** The [turn]th tip of this unlock (the screen turns one every few seconds), from [seed]. */
    fun tip(turn: Int, seed: Int): String = tips[Math.floorMod(seed + turn, tips.size)]

}
