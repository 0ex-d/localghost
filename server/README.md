# ghost.secd

The keystone of a LocalGhost box. ghost.secd is the front door and the trust boundary: every request
from outside terminates here, gets authenticated, is bound to an unlocked account, and is proxied to
the right backing daemon. The other daemons (ghost.tallyd, ghost.voiced, ghost.shadowd, and the rest)
listen only on loopback and are never exposed. Auth, account selection, and the duress/wipe logic all
live at this one chokepoint, so a daemon can only ever serve the account that is currently mounted.

```
internet ──TLS──> nginx ──> ghost.secd (authn + account routing) ──loopback──> ghost.<x>d
```

This document is the whole picture. The security model comes first because everything else follows
from it.

---

## Threat model , what this defends and what it does not

Be honest about the boundary, so it can be designed to instead of hoped for.

Defended:
- **Seize the box once.** Someone takes the box and images the disk. Equal-size containers, a
  count-hidden registry, and per-account crypto-erase mean they cannot tell how many accounts exist,
  which is real, or which holds your data. This is the core threat and it is solid.
- **Stolen phone / rogue enrolled device, no box root.** Brute-force lockout, Argon2id, constant-time
  checks. A guesser is rate-limited and locked out and cannot read the credential.
- **Coercion to unlock.** A duress PIN opens a believable decoy and silently crypto-erases the main;
  a wipe PIN burns everything. Both look identical to a wrong PIN to an onlooker.

Not defended, stated plainly:
- **Root on the box, live.** Root can read decrypted data while an account is mounted, scrape a PIN
  in memory during a real unlock, or patch the daemon. Hardware (the TPM) protects keys at rest and
  rate-limits guessing; it does not stop active root watching a live unlock.
- **Repeated imaging over time.** Equal-size containers defeat a single snapshot. An attacker who
  images the box repeatedly sees the active account's container churn while decoys sit still. Hiding
  that needs ORAM-class oblivious writes and is out of scope.
- **The expert adversary, behaviourally.** Against someone who knows the LocalGhost design, the
  deniability that survives is structural (they cannot count your accounts), not behavioural (no
  "this decoy looks used, so it is real" argument, which they would discount anyway). We deliberately
  do not ship features that manufacture behavioural cover; they only add config to explain.

Deniability stance: decoys are real, separately-seeded accounts that are stale because you do not use
them, and we never claim otherwise. "I do not use that account much" is unfalsifiable; "look how
active it is" invites suspicion precisely because the system could fake it. Stale-and-honest beats
synthesized-and-suspicious.

---

## Access model , key first, PIN second

Two independent gates, in this order:

1. **Device cert (the key).** The box is its own CA and is always HTTPS. At setup it mints a client
   certificate for the phone; the phone receives it by scanning the QR (the box generates the key,
   the phone does not). nginx is configured with `ssl_verify_client on` against the box CA, so any
   connection without a box-issued cert is rejected at the TLS handshake , before it reaches
   ghost.secd, before any account or PIN. A scanner hitting the public IP gets a handshake failure
   and learns nothing. Reachability is not access.
2. **Account PIN.** Only once the device is trusted does ghost.secd care which account, proven by the
   PIN, with the duress/wipe logic.

Both must fall for access: a stolen phone still needs a PIN; a known PIN is useless without an
enrolled device cert. This is why a public IP is safe , the door only opens for a key delivered by a
QR you physically showed, over your own SSH session.

Issuance is privileged: only the box `ghost`/root user can mint a device cert, so an attacker cannot
enroll their own phone remotely. The QR therefore carries a secret (the device private key), which
makes the QR itself a credential , shown once, over SSH, never stored. Setup clears it afterwards.

## Setup flow , `ghost.secd setup` (run as root)

Two phases, so nothing destructive happens until you have seen and confirmed the whole plan
(`setup/steps.go`, `setup/plan.go`):

1. **Dry run.** Walks every step WITHOUT touching anything: prints what it would do (including
   "partition /dev/X, this erases it"), checks preconditions (disk, nginx installed, DNS resolves,
   TPM usable), and stops you here if anything is wrong. Destructive steps are guarded so they can
   never run in this phase.
2. **Apply.** Only after a clean dry run and your confirmation. Runs in order, skips already-done
   steps (re-runnable), and stops at the first failure rather than half-provisioning.

What setup does, in order:

```
partition disk              GPT + equal-size LUKS containers (DESTRUCTIVE, apply-only)
format equal-size containers  one per account slot (main + 2 decoys)
ghost user                  unprivileged system user the daemons run as
box CA (self-signed)        the box becomes its own certificate authority
box server cert             the box's https cert, signed by the box CA, pinned by the phone
device cert                 the phone's client cert, delivered via the QR
nginx installed             checked, not installed for you
dns points at box           only with a domain; verified (reachability only, not cert issuance)
nginx config                mTLS: reject any client without a box-issued cert at the handshake
nginx reload
install systemd services    ghost.secd + every ghost.<x>d daemon, hardened units
enable + start services
tpm usable                  warns if not
clear setup artifacts       the QR carried a key: wipe history, temp files, the rendered QR
harden console              local console unusable as a bypass
```

All the operator does beforehand: point the chosen subdomain's A record at the box IP. Setup does
the rest. Validated: dry-run touches nothing, apply refuses a dirty dry run, destructive steps never
run in preview, apply stops at first failure, and the systemd units are hardened and correctly
ordered (daemons require ghost.secd; only ghost.secd gets TPM access).

## Certificates , the box is its own CA (no Let's Encrypt)

The box issues its own server cert and the phone's device cert from one self-signed CA. The phone
PINS the box cert (the fingerprint travels in the enrollment QR) and trusts that CA only. This is
stronger than Let's Encrypt for a personal box, not weaker: there is no public CA that could
mis-issue a cert for your domain, the phone trusts exactly one key , yours. It also removes the
Let's Encrypt operational fragility entirely: no port 80, no ACME challenge, no 90-day renewal that
breaks when your IP changes. The app pins the cert in its networking layer (a custom TrustManager /
CertificatePinner seeded from the QR), so self-signed is invisible to it , the "self-signed is
scary" reputation is a browser concern, and the app is not a browser visiting arbitrary sites. If
you ever want plain-browser access you can add Let's Encrypt for that path only; the app always pins.

## Domain and DNS , optional, with a privacy cost

`setup/domain.go` takes a domain (yours, or a name under localghost.ai), checks the A record resolves
to the box BEFORE standing anything up (`VerifyDNS`), and only then renders the nginx server block
(`NginxConfig`). nginx terminates public TLS and forwards to ghost.secd on loopback; it never sees
decrypted data or does auth, that all happens inside ghost.secd.

The honest trade is now small, because of the access model above. A public domain makes the box's
existence and address resolvable, and whoever runs the DNS zone can see the box's IP , but resolving
the box grants nothing, since nginx rejects every connection without a box-issued device cert at the
handshake. So a public name reveals "a box exists here and speaks mTLS", nothing more, and no account
or data. With no domain the box just lives on the LAN (the QR carries the LAN host); remote access is
opt-in later by adding a domain, and the cert gate is already there when you do. Hiding even that a
TLS service exists would need port-knocking or a VPN in front, a later option, not now.

---

## Gateway , the single front door

`gateway/` is the reverse proxy. The other daemons bind loopback only; ghost.secd authenticates every
request (mTLS plus the unlocked account) and routes by service name to the right daemon for the
MOUNTED account (`Router.Resolve`). A locked box refuses all routing (`ErrLocked`), so no daemon is
reachable until an account is open, and a daemon only ever receives requests for the account that is
mounted. A mounted decoy routes to the same daemons, serving the decoy's own data.

---

## Auth , brute-force defence

`auth/` rate-limits PIN attempts with escalating delay and a hard lockout, stores only an Argon2id
hash, and compares constant-time. `Gate.CheckAllowed` / `RecordSuccess` / `RecordFailure` drive the
rate limiting where validity is decided by the profile registry. This fully defends a phone attacker
with no box root. Against box root it is bypassable; the TPM is the real defence there (`auth/tpm.go`).

---

## Profiles , the 3-slot policy

`profile/setup.go` enforces the layout so an invalid one cannot be built:

```
slot 0  main account (your real data)
slot 1  decoy (believable data, no wipe) , the casual show
slot 2  decoy that wipes the main on open , the duress show
```

All three open believable accounts; everything else is invalid (a mistype is rejected, never
destructive). `Accounts.Unlock` rate-limits, resolves the PIN constant-time against the count-hidden
registry, then opens the slot, firing the main-account wipe first for the duress decoy. An Open looks
identical whether main or decoy, so a coercer cannot tell which they got. The registry always holds a
fixed number of entries, real ones padded with random filler, so the number of real PINs never leaks.

---

## Containers , equal size so none looks bigger

`container/` makes each account a fixed equal-size encrypted container. The real account fills its
container with data plus padding; decoys fill the SAME size with believable data plus padding. From
outside, all are identical ciphertext blobs and the internal fill level is invisible. `Layout.Verify`
hard-rejects any odd-sized container. Growth is keyless: `GrowAll` appends random bytes to every
backing store (no key, since random is indistinguishable from ciphertext), so the daemon grows all
three in lockstep when the main fills WITHOUT holding the decoy keys. Each filesystem extends into the
new space lazily at mount via `Mounter.ResizeToFill`, with its own key. The account keys stay fully
independent: nothing derives a decoy key from the main and no account can unlock another. Mount time
is uniform (TPM unseal plus key setup, not bulk decrypt), so a fuller account does not mount slower.

It is just Postgres and Redis state that differs per account, not a user-facing filesystem; each
account's data lives in its own container and the daemons run only against the mounted one.

---

## Integrations , per account, decoys paused

`integration/` makes connectors (bank, calendar, email, cloud) part of EACH account's encrypted
store, never global. A connector's tokens decrypt only when its account is mounted, sealed under that
account's key, so a decoy literally cannot reach the main's bank tokens. A decoy holds integrations
as Paused (configured, not polling), because a live, refreshing connector is a behavioural tell a
stale account would not show. `Add` starts Paused, `Enable` works only on the main and is refused on a
decoy, and `Load` forces decoy integrations Paused even if the stored blob says otherwise. A Set is
built only from the mounted account's own bytes, so it can never cross accounts.

---

## Wipe , per-account crypto-erase

`wipe/` makes destruction targeted and forensically robust. You cannot reliably overwrite flash (wear
levelling, over-provisioning), so the wipe does not try. Each slot has its own account master key
(AMK) sealed in the TPM; a slot's data key needs BOTH that AMK and the slot's PIN key. A duress decoy
destroys only the main slot's AMK (`WipeAccount`); `PanicWipe` destroys all. After the AMK is gone the
slot's ciphertext is permanently undecryptable, defending even an attacker who imaged the disk earlier
and coerces a PIN later. Keys live in mlock'd buffers (`zeroize.go`) so they never swap and are
zeroised on wipe and unmount.

---

## The hardware seam (the next milestone)

One backend turns this from validated logic into a running system: the TPM and dm-crypt integration.
On this box (bare-metal Debian 13, Intel PTT) there is no VM, so no vTPM/hypervisor gap , the strong
case. The seam covers:
- `auth/tpm.go SealedKey` , seal/unseal the account keys, PIN-gated, with the TPM's hardware lockout.
- `wipe/destroy.go HardwareEraser` , evict an account's sealed key (per-account crypto-erase) or all.
- `container/container.go Mounter` , dm-crypt map the fixed-size container, `appendRandom` to grow,
  `ResizeToFill` per account.

Until it is wired, `ErrNoHardwareErase` is raised so you know a wipe is best-effort. Intel PTT is a
firmware TPM, which raises the bar enormously without being absolute against physical attack.

---

## Layout

```
auth/         brute-force gate, Argon2id credential, TPM seam
profile/      registry (constant-time, count-hidden), 3-slot setup policy, Accounts.Unlock
container/    equal-size containers, keyless lockstep growth, mount seam
integration/  per-account connectors, decoys paused
wipe/         per-account crypto-erase, mlock'd secrets, hardware-erase seam
gateway/      reverse proxy: loopback daemons behind one authenticated front door
hw/           TPM seal/unseal, dm-crypt mount, per-account Postgres/Redis (real hardware, -tags tpm)
models/       phone-runnable model catalogue + byte server (unencrypted, shared)
admin/        PIN rotation (resetup-*), fail-closed local-network gate, sshd binding check
setup/        ordered idempotent setup plan, device-CA/nginx/DNS/cleanup steps, domain verification
pair/         self-contained QR enrollment (no server)
cmd/ghostctl/ CLI client: enroll from a link
```

## Build and test

```
go test ./...
```

No external services are required for the tests. The security-critical logic (registry resolution,
per-account crypto-erase, the unlock decision tree, DNS verification, routing) is unit-tested; the
hardware and dm-crypt pieces are the seams above, tested on the box.

## Dependencies

One, deliberate: `golang.org/x/crypto` for Argon2id (the one password KDF that is not in the standard
library and must not be hand-rolled). Everything else , AES-GCM, HKDF (stdlib `crypto/hkdf` on go
1.24+), X.509/mTLS, SHA-256, the QR encoder (own implementation in `pair/qrencode.go`) , is the
standard library. The real box build additionally links `github.com/google/go-tpm` behind the `tpm`
build tag; the default build stays on the single dependency above.


## Unlock progress , streamed, and identical across accounts

A cold unlock is not instant: the container mounts (TPM unseal + dm-crypt), then the account's
Postgres and Redis start. `profile/unlock_stream.go` streams the stages (checking, unsealing,
mounting, starting database, starting cache, starting services, ready) so the app shows a real
loading state instead of a freeze.

The stream is IDENTICAL for every account. A duress unlock emits the same stages, in the same order,
with the same labels, as a real one, so the progress reveals nothing about which account is opening
(validated: two cold unlocks are byte-identical). The only legitimate variation is warmth: an
already-mounted hot account reports the heavy stages as Skipped (skip, skip, skip, fast), a cold one
runs them. Warmth tracks how recently you used the box, not which account is real, so it is not a
tell , and it is why a hot unlock is fast while a cold one honestly takes its time.

This also gives the cold start an honest place in the product: it is just how the box starts a cold
account, neither hidden nor dramatised. The deniability under coercion lives in the duress PIN (opens
a believable decoy, wipes the main) plus whatever you choose to say , not in the timing. The system
does not claim anything; it loads honestly and the same way every time.


## PIN rotation , `ghost.secd resetup-*` (box-only, local network)

There is no PIN change without data loss, by design. A PIN is bound to its volume's key, so rotating
a PIN means destroying the old key (the data is crypto-erased) and creating a fresh volume keyed by
the new PIN. Reset equals wipe. This is deliberate: it means there is no rotate-in-place admin
surface for an attacker to abuse, and no way for a coerced phone to rotate anything.

Rotation is reachable ONLY from the box, over a local-network session , never from the app, never
remotely:

```
ghost.secd resetup-main      erase + re-create slot 0 with a new PIN
ghost.secd resetup-decoy     erase + re-create slot 1 (the plain decoy) with a new PIN
ghost.secd resetup-duress    erase + re-create slot 2 (the duress decoy) with a new PIN
```

Each command names its slot explicitly and touches only that slot , no relative roles, no cross-slot
indirection. The gate (`admin/local.go`, validated) FAILS CLOSED: it reads the session's real peer
address and refuses unless it is provably loopback/RFC1918/link-local; anything public, ambiguous, or
undeterminable is rejected. Defence in depth: setup also verifies sshd is not listening on a public
interface (`admin/sshd.go`) and warns if it is.

The safety ordering is the important part (`admin/resetup.go`, validated): the command shows
"you are about to ERASE the <slot> partition (<size>), this is permanent", then takes the new PIN
TWICE, and destroys the old key ONLY after the new PIN is entered and confirmed. A typo or mismatch
aborts and leaves the slot untouched, so a slip never costs the volume. The PIN is zeroised from
memory on every path. CommitReset wipes and re-creates atomically, so there is no window with neither
key.

Honest limit: "local network" means a private/loopback source address, not "physically on my LAN". A
VPN or port-forward landing a peer in a private range would pass. For this threat model that is
acceptable (anyone already on the LAN/VPN has broad access); tighten to loopback-only (SSH to
localhost / console) if you want to close even that.


## Models , unencrypted, shared across accounts

`models/` serves phone-runnable models. They live in the UNENCRYPTED system area (e.g.
`/var/lib/ghost/models`), beside where the apps run, NOT inside any account's container. Models are
public artifacts identical for every account , the same as the daemon binaries , so encrypting them
per slot would buy nothing and cost three copies of multi-gigabyte weights. Reading or serving a
model therefore needs no mounted account and no PIN, and carries no personal data, so it does not
weaken per-account isolation.

ghost.secd reads the catalogue (`catalog.json`: id, name, detail, sizeBytes, sha256) when the phone
asks what is available, sorted smallest-first since those run best on a phone, and streams a model's
bytes for the phone to download and run locally. The byte server validates the id against the
catalogue and refuses any path that escapes the models dir (traversal guard), and the catalogue
carries a SHA-256 the phone verifies after download (and `Verify` lets the box confirm an installed
model is intact). Validated.


## Unlock: simulation by default, real hardware with -tags tpm

The unlock flow (PIN to a mounted, running account) goes through the `UnlockBackend` seam
(`server/backend.go`). There are two implementations selected by build tag, so ghost.secd compiles
and the app's unlock flow is testable on any machine, while the real hardware path is a one-flag
switch on the box:

  go build ./...            default: simulation backend (server/backend_sim.go). Opens the main slot
                            for any non-empty PIN and simulates the cold-unlock cost. NOT a security
                            boundary , for development and the app's loading UI off-box only.

  go build -tags tpm ./...  real backend (server/backend_tpm.go). Wires:
                              profile.Accounts   PIN resolution (real / decoy / duress-wipe / reject)
                              hw.TPMSealedKey     per-slot seal/unseal against /dev/tpmrm0 (Intel PTT)
                              hw.DMCryptMounter   LUKS map + filesystem mount of the slot container
                              hw.DataStore        per-account Postgres + Redis inside the volume
                            Needs go-tpm in the module (go get github.com/google/go-tpm), a TPM,
                            root, cryptsetup, postgres and redis. Exercise on the box.

The shared stage logic (`runUnlock`) lives in one place so the sequence and timing-uniformity (a
duress unlock looks identical to a real one) are the same in both builds. The key is unsealed once
and zeroised right after the mount consumes it.

The account registry is persisted by `profile.Registry.Save` / `LoadRegistry` (`profile/persist.go`)
in the unencrypted state area. The on-disk form holds ALL RegistrySize entries , real and random
filler , in a fixed-size layout, so the file never reveals how many real PINs exist (the count-hiding
property that gives the deniability its teeth survives a restart). It stores only salted PIN hashes,
never PINs or keys. Validated: round-trip preserves every PIN's resolution incl the duress wipe
target, and the file size is identical for one real PIN or many.

Honest limit, restated: the TPM and dm-crypt paths are built against the documented go-tpm and
cryptsetup interfaces and are NOT validated in CI (no TPM, no root, no encrypted volumes in the build
env). They must be exercised on xyntai.
