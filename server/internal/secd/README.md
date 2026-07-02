# internal/secd

ghost.secd is the only thing the outside world can reach, and it owns nothing worth stealing. That is
the whole idea. It takes a PIN, and if the PIN is right it asks the TPM to unseal the disk key, opens
the encrypted drive, and brings the account to life. If the PIN is wrong it goes quiet in a way that
looks exactly like a box that is switched off. Everything that matters lives behind a door this service
can open but cannot see through on its own.

So when you read this package, hold one thing in mind. A thief who walks off with the box and reads
every byte of ghost.secd plus the unencrypted state gets the doorman's source and a list of salted PIN
hashes. They do not get a single note, photo, or message, because none of it is here. It is all on the
far side of a LUKS volume that needs a PIN-gated TPM unseal this code cannot perform without the person.

## What it is responsible for

Being the front door, and nothing more. It runs enrolment (how a phone first earns a device cert),
unlock (PIN in, drive open or appear-down), the session token that the rest of the app rides on, and
the notification surface. It holds the request handlers; the real work happens in the packages it
drives (profile decides what a PIN means, hw touches the hardware).

The build splits in two. The default build stubs the TPM and the mount with sleeps so the HTTP flow can
run end to end on a laptop with no hardware. The `-tags tpm` build wires the real TPM, dm-crypt, and the
per-account databases. The HTTP surface, sessions, and notifications are the same in both.

## The two things that are easy to get wrong

The session token and the poller share one credential, on purpose. A wrong PIN revokes it, and that
darkens the foreground and the background poll together. If they ever drift onto separate credentials, a
seized phone could show the app "down" while notifications keep arriving, which gives the whole game
away. Keep them shared.

And `/v1/health` stays honest. It is how the person checks their own box is alive. Route it through the
appear-down path and you have blinded yourself along with the attacker.

## Honest state

The real backend has never run. The sim build exercises the flow, but the TPM unseal, the mount, and the
per-account Postgres and Redis are structurally checked and nothing more. The daemons that produce
notifications are still stubs, so the produce side is waiting on them; the read, mute, seen, and delete
side is built. The box compiler and a real unlock are what will tell us the truth.
