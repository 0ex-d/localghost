# internal/profile

This package answers one question. What does this PIN mean? It takes a PIN and returns a decision, open
the account, wipe everything, or reject, and it does it without touching hardware so the logic stays
pure and testable. The decision is the easy half. The hard half is making sure the shape of the answer
never leaks anything, which is most of what this package is about.

There are two PINs. The main PIN opens the one real account. The wipe PIN crypto-erases everything and
then looks like a wrong PIN, so an onlooker watching you enter it sees a failed unlock and nothing
more. A genuinely wrong PIN also rejects. From the outside the wipe and the typo are the same event.

There are no decoys. We took them out. Equal-size decoy containers buy nothing against someone who
images the disk once your real data passes a third of the volume, and they leak through block
allocation anyway. Deniability moved to the phone (a thin client holding only recent data) and to the
app layer (the box can appear down). The reasoning lives in the threat-model docs and it is worth
reading before anyone is tempted to add slots back.

## Where the leaks would be, and how they are closed

The registry is a fixed size, padded with random filler, so the number of real PINs never shows, and
neither does the fact that a wipe PIN exists at all. Resolve runs in constant-ish time so the duration
of the check does not tell you which PIN went in. The unlock stage stream is identical for every
outcome, so a wipe and a normal open emit the same stages in the same order. Three different places, one
goal, which is that the only thing an observer learns is the eventual outcome, and the appear-down layer
hides even that.

One thing is deliberately not enforced in code. The main and wipe PINs should be nothing alike, but that
is guidance to the person, not a rule the software checks, because a rule that rejected similar PINs
would itself be a tell about how the PINs are used.

## Honest state

The decision logic is the most finished part of the whole system and it is property-tested. Round-trip
persistence keeps every PIN's meaning, the blob stays a fixed size whether one PIN is set or two, and
the stage stream stays identical across outcomes. What it never sees is the AMK or the disk. It maps PIN
to decision and hands off; the unseal and the mount happen in hw, driven by secd.
