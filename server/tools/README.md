# First-time box setup, start to finish

Who runs what, in order. Two users: `root` for anything that touches the system (packages, the
ghost user, the disk, nginx, systemd) and your normal user (`coder` here) for building and tests.
The data disk in these examples is `/dev/nvme1n1` , the clean 8TB NVMe with NO partitions in
lsblk. Setup will destroy whatever is on the disk you pass it. Read the plan output before apply.

## 0. Prerequisites (root, once)

- fTPM enabled in the BIOS (Intel PTT on this board). Verify: `ls /dev/tpm*` shows `/dev/tpmrm0`.
- The domain's A record pointing at your public IP, and your router forwarding TCP 443 to this
  box's LAN address, if you want remote access. LAN-only works without either.
- The repo on the box, readable by both users.

## 1. System prep , root

    cd server
    ./tools/server_setup_root.sh

Packages, the `ghost` user, directories. Idempotent; safe to re-run.

## 2. Build + tests , coder

    cd server
    ./tools/server_setup_user.sh

Builds everything into `./bin` and runs the test suite. The real box needs the TPM backend, so if
the script built the default (sim) binaries, rebuild the daemon explicitly:

    go build -tags tpm -o bin ./...

The sim backend is for development; on the box, an accidental sim build means your PINs guard a
simulation while the real disk sits unsealed. Check before proceeding.

## 3. Dry run , root

    ./bin/ghost-setup --disk /dev/nvme1n1 --host <LAN-IP> --domain lgs.vladcealicu.com --plan

Prints every step it would take and touches nothing. The one line to read twice is the partition
step: confirm the device is the empty NVMe (`nvme1n1` here), not the OS disk (`nvme2n1`), not the
bitcoin SSD (`nvme0n1`), not the ZFS pool members (sda-sdf). There is no undo for picking wrong.

`--host` is the LAN address the phone enrols against; `--domain` adds the public nginx config and
verifies DNS. The server cert is issued for the host, but the app pins the certificate FINGERPRINT,
not the name, so reaching the same box via the domain later works by design.

## 4. Apply , root

    ./bin/ghost-setup --disk /dev/nvme1n1 --host <LAN-IP> --domain lgs.vladcealicu.com --apply

Prompts for the main PIN and the wipe PIN on the tty (no echo, nothing in shell history). Pick them
now; make them different; the wipe PIN destroys everything and then lies about it, which is the
point. Apply partitions the disk, creates the CA (issuer CN is a deliberately boring "ca"),
issues the server cert, writes nginx and the systemd units, starts the daemons, and renders the
enrolment QR.

If the DNS verification fails because the domain resolves to your PUBLIC IP while the box only
knows its LAN address (NAT), re-run without `--domain`, finish the LAN setup, and add the domain
config afterwards; the enrolment itself never needed the domain.

## 5. Enrol , phone

Scan the QR from step 4 with the app. Scanning IS enrolment: the QR carries the device certificate
and key, there is no pairing code and nothing to confirm on the box. The QR is therefore a
credential , clear the terminal (or close the SSH session) once the phone has it. Need a fresh QR
later? `./bin/ghost-qr --ca /etc/ghost/ca --host <LAN-IP>` as root; each run mints a fresh device
identity, so always scan the newest one.

Then unlock with the main PIN in the app. A wrong PIN looks exactly like a down box; that is not a
bug report, that is the product.

## 6. Models , root

    mkdir -p /var/lib/ghost/models
    cp <model>.gguf /var/lib/ghost/models/
    sha256sum /var/lib/ghost/models/<model>.gguf

Add an entry to `/var/lib/ghost/models/catalog.json`:

    [{"id": "qwen-1.5b", "name": "Qwen 1.5B", "detail": "small local model",
      "sizeBytes": 1234567890, "sha256": "<the hash>"}]

The app's Models screen lists the catalogue; downloads resume across drops (Range requests). The
box never fetches models from the internet itself , you put them there, deliberately.

## The app

One blocker before it builds: the llama.cpp pin in `app/src/main/cpp/CMakeLists.txt` is a
placeholder and CMake fails loudly until you fill it. On any trusted machine:

    git ls-remote https://github.com/ggml-org/llama.cpp refs/tags/b9788

Paste the full 40-char SHA into `LLAMA_CPP_COMMIT`, then build per COMPILE.md (first native build
compiles ggml for arm64 , it takes a while). Install the APK, and you are at step 5.

## Undo

`./tools/server_setup_undo.sh` (root) walks the system pieces back. It does not resurrect data on
the disk you partitioned; nothing does, which is also the product.
