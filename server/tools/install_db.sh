#!/usr/bin/env bash
# Install Postgres + Redis BINARIES for LocalGhost, then make sure NOTHING runs them automatically.
#
# Why this is not a normal install: ghost.secd is the process supervisor, not systemd. Postgres and
# Redis run as PRIVATE instances that ghost.secd starts AFTER the encrypted volume is unlocked and
# mounted , their data dirs, ports, and passwords all live inside that volume (see services.conf).
# The Debian packages want to auto-create a cluster on the system drive and enable a boot-time
# systemd unit; both are wrong here, so we install the binaries and then DISABLE + MASK the units and
# REMOVE the auto-created Postgres cluster. A packaged instance on the system drive would (a) hold a
# default port ghost.secd's private instance must avoid, and (b) put unencrypted DB data on the
# system disk , exactly what this design refuses.
#
# Coexistence: if you ALREADY run a system Postgres/Redis on 5432/6379 for something else, this does
# not touch it , masking only stops the DISTRO units from auto-starting the ones we just installed.
# ghost.secd's instances use their own ports (services.conf), so they never collide.
#
# Idempotent. Reads nothing secret. Run as root. Read it before you run it.
set -euo pipefail

. /etc/os-release
CODENAME="${VERSION_CODENAME:?cannot determine Debian codename}"
ARCH="$(dpkg --print-architecture)"
echo "> Debian ${CODENAME} (${ARCH})"

echo "> prerequisites..."
apt-get update
apt-get install -y curl ca-certificates gpg lsb-release

# --- PostgreSQL (PGDG, Postgres 18 , newer than Trixie's default 17) --------------------------
echo "> adding PGDG repository (deb822)..."
install -d /usr/share/postgresql-common/pgdg
curl -fsSL -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc \
    https://www.postgresql.org/media/keys/ACCC4CF8.asc
cat > /etc/apt/sources.list.d/pgdg.sources <<EOF
Types: deb
URIs: https://apt.postgresql.org/pub/repos/apt
Suites: ${CODENAME}-pgdg
Components: main
Architectures: ${ARCH}
Signed-By: /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc
EOF

# --- Redis (official redis.io repo, Redis 8.x , newer than Trixie's default) -------------------
echo "> adding Redis repository..."
curl -fsSL https://packages.redis.io/gpg | gpg --dearmor -o /usr/share/keyrings/redis-archive-keyring.gpg
chmod 644 /usr/share/keyrings/redis-archive-keyring.gpg
cat > /etc/apt/sources.list.d/redis.sources <<EOF
Types: deb
URIs: https://packages.redis.io/deb
Suites: ${CODENAME}
Components: main
Architectures: ${ARCH}
Signed-By: /usr/share/keyrings/redis-archive-keyring.gpg
EOF

# Preseed BEFORE the packages land: postgresql-common auto-creates a 'main' cluster at install time
# unless told not to. With this, no cluster is ever created on the system drive , the drop further
# down becomes belt-and-braces for boxes that installed before this preseed existed.
echo "> preseeding: no auto-created cluster..."
install -d /etc/postgresql-common
if ! grep -qE '^\s*create_main_cluster\s*=\s*false' /etc/postgresql-common/createcluster.conf 2>/dev/null; then
    printf '\n# LocalGhost: clusters live on the encrypted volume, never the system drive\ncreate_main_cluster = false\n' \
        >> /etc/postgresql-common/createcluster.conf
fi

echo "> installing binaries..."
apt-get update
# postgresql-18 pulls the server + client + postgresql-common; postgresql-18-pgvector is the search
# layer's vector index (rides into the volume bundle later). redis-server + redis-tools give the
# server binary and redis-cli. We install, then immediately neutralise the auto-provisioning below.
apt-get install -y postgresql-18 postgresql-client-18 postgresql-18-pgvector redis-server redis-tools

# --- Neutralise auto-provisioning -------------------------------------------------------------
echo "> stopping + masking distro units (ghost.secd owns these lifecycles)..."
# Postgres: the packaged unit is a wrapper that starts ALL clusters. Stop + disable + mask it and
# the per-version template so nothing brings a cluster up at boot.
systemctl stop    postgresql.service            2>/dev/null || true
systemctl disable postgresql.service            2>/dev/null || true
systemctl mask    postgresql.service            2>/dev/null || true
# Redis: same , the packaged redis-server.service must never auto-start our binary.
systemctl stop    redis-server.service          2>/dev/null || true
systemctl disable redis-server.service          2>/dev/null || true
systemctl mask    redis-server.service          2>/dev/null || true

echo "> removing the auto-created Postgres cluster on the SYSTEM drive (if any)..."
# postgresql-common creates a 'main' cluster at install. It lives on the system drive with
# unencrypted data , exactly what we do not want. Drop it. ghost.secd will initdb a fresh cluster
# INSIDE the encrypted volume at provision. pg_lsclusters lists what exists; pg_dropcluster removes.
if command -v pg_lsclusters >/dev/null 2>&1; then
    # Only the auto-created 18/main on the system drive; never touches an encrypted-volume cluster
    # (which is not registered with pg_lsclusters , ghost.secd runs postgres directly by -D).
    if pg_lsclusters -h 2>/dev/null | awk '{print $1"/"$2}' | grep -qx "18/main"; then
        pg_dropcluster --stop 18 main || true
        echo "  dropped 18/main"
    else
        echo "  no 18/main cluster to drop"
    fi
fi

# Redis leaves /var/lib/redis and a default /etc/redis/redis.conf. We leave the files (harmless,
# unused , ghost.secd writes its own redis.conf inside the volume) but the masked unit guarantees
# the distro config never starts a server on the default port.

echo
echo "> done. Installed versions:"
/usr/lib/postgresql/18/bin/postgres --version 2>/dev/null || postgres --version 2>/dev/null || true
redis-server --version || true
echo
echo "Binaries are installed; NOTHING is running. ghost.secd starts private instances against the"
echo "encrypted volume at unlock, using ports + credentials from <mount>/services.conf."
echo "If you run a SEPARATE system Postgres/Redis for other apps, it is untouched , this only masked"
echo "the units for the packages we just installed."
