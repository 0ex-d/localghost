# Security Policy

This document covers how to report a vulnerability in LocalGhost. For the security architecture as built (the encrypted volume, the TPM seal, the PINs, the client certificates, the signed mirror), see [the release notes](server/releases/0.0.1.md) and the [README](README.md#the-vault-and-the-pins); for the design it is built towards (decoy volumes, the duress flow, the purge) and the threat model in longer form, see [The Honeypot Under Your Desk](https://www.localghost.ai/hard-truths/honeypot). The gaps already known are listed in the [release notes](server/releases/0.0.1.md#known-gaps-at-wisp).

## Reporting a Vulnerability

Email `info@localghost.ai`, encrypted with our [PGP key](https://www.localghost.ai/.well-known/pgp-key.asc).

Please include:

- A description of the vulnerability and what it allows
- Steps to reproduce
- The LocalGhost version or commit affected
- Any proof-of-concept code or exploit details
- Your preferred credit line if you want to be credited

## What Happens Next

Acknowledgement within 72 hours. A real response (assessment, proposed fix, timeline) within 14 days. If the fix is going to take longer than 14 days, you'll get a reason why and a revised timeline.

## Disclosure Timeline

We ask for 90 days before public disclosure, or until a fix ships, whichever comes first. If a vulnerability is being actively exploited in the wild, we'll coordinate faster. If we can't fix in 90 days, we'll tell you why and agree an extension with you rather than silently let it slide.

## Scope

In scope:

- Any daemon in the LocalGhost fleet (`ghost.secd`, `ghost.watchd`, `ghost.framed`, `ghost.noted`, `ghost.voiced`, `ghost.tallyd`, `ghost.synthd`, `ghost.cued`, `ghost.shadowd`, `ghost.searchd`, `ghost.oracled`) and the operator's tools (`ghost-ctl`, `ghost-cli`, `ghost-setup`, `ghost.restore`, `ghost-update-guard`)
- The Android app, its enrolment (the QR) and its sealed stores on the phone
- The setup, update and release tooling (`server/tools/`), the mirror's manifest and its verification
- Documentation that describes security-relevant behaviour incorrectly (that's a vulnerability in the instructions, which is still a vulnerability)

Out of scope:

- Vulnerabilities in dependencies we don't maintain (Postgres, Redis, LUKS, llama.cpp, whisper.cpp, go-tpm, the Android platform). Report those upstream. If one affects LocalGhost specifically, tell us so we can pin or patch.
- Social engineering attacks against the project or its contributors
- Physical attacks that require prior undetected access to the box for an extended period (cold boot attacks on a running box are in scope, cold boot attacks after an attacker has had it for a week are not)
- Denial of service that requires local root access

## What We Won't Do

**Pay a bounty.** One person builds this. If the project grows and funds a bounty program later, we'll say so. We will credit you publicly with your permission, and we will take your report seriously.

**Sue you for responsible disclosure.** If you find a vulnerability, report it in good faith through the process above, and give us a reasonable window to fix it, we will not pursue legal action against you. This is a commitment, not a formality.

**Hide vulnerabilities after fixing them.** Post-fix, we publish an advisory covering what was found, what was affected, what shipped. The security of the system depends on the architecture being public.

## Not a Vulnerability

Some things get reported that aren't vulnerabilities. Flagging the common ones so we can handle them efficiently.

- **"The wipe PIN can be defeated by a nation-state adversary with prior knowledge of the architecture"**. Yes. The architecture is built for border agents and wrench attacks, not state-level forensics; the [Honeypot](https://www.localghost.ai/hard-truths/honeypot) essay says so at length.
- **"The decoy volume and the duress PINs the essays describe are not in the code"**. Correct, and said in the README and the release notes. They are the design the vault is built towards; wisp ships one PIN and one wipe PIN.
- **"The project publishes its source code so attackers can read it"**. That's the design. [Kerckhoffs's principle](https://en.wikipedia.org/wiki/Kerckhoffs's_principle). Security through obscurity is not a property we're trying to have.
- **"Someone could steal the hardware"**. Yes, which is why the volume is encrypted with a key sealed to the TPM (or to the PIN through Argon2id) and why the wipe PIN exists. If you find a way to extract data from a seized box without the PIN, that is a vulnerability, tell us.

## PGP Key

```
Available at https://www.localghost.ai/.well-known/pgp-key.asc
Fingerprint: DCE9 A3D1 4EB4 6197 1DD5  F393 706E 4194 F08A 09A0
```

The same key signs the mirror's manifest and every release's `SHA256SUMS` and APK, and is
committed in the repo as `server/tools/mirror-key.asc`, pinned by this fingerprint in
`server/tools/mirror_fetch.sh`.

Verify before you send anything sensitive. If the fingerprint on the key doesn't match a trusted out-of-band source, don't encrypt to it.
