#!/bin/sh
# fetch_geo.sh <dest-dir> , downloads the public geodata the box geocodes and draws maps with:
# GeoNames (CC-BY: allCountries + admin1/admin2 code names + countryInfo), the Natural Earth
# countries GeoJSON (public domain) and OpenStreetMap's coastline (ODbL), from the localghost.ai
# mirror and nowhere else (GHOST_MIRROR_UPSTREAM=1: each upstream, on the operator's own authority).
# ~400MB compressed, one-time, at SETUP , before any
# personal data exists, so the only thing revealed is "this IP provisioned a box once", the same
# class of disclosure as the apt installs setup already performs. NEVER run this against personal
# coordinates or from a running box's context; the whole point of on-box geocoding is that photo
# coordinates never touch the network.
#
# Best-effort per file: a miss is a loud note, not a failure , the box works without geo data, and
# `ghost-cli ghost.framed geo-import` picks up whatever the operator drops in later.
set -u
# UPDATES (tools/update.sh runs this on a running box): a set installed from the mirror leaves a
# record (<geo>/.mirror-geo, <shapefile dir>/.mirror-landpolygons, <geo>/roads/.<file>.sha256), and
# when the mirror's current build lists other bytes the set is fetched again. A set installed
# before records existed (or from an upstream) is kept as it is and said so; GHOST_GEO_REFRESH=1
# re-downloads everything regardless. What changed is listed in <geo>/.fetch-geo-changed for the
# caller: geo, landpolygons, roads (fetched), landtiles, roadtiles (want cutting); new names want
# `ghost-cli ghost.framed geo-import` (upserts), the tiles geo-tiles / road-tiles. GHOST_GEO_NO_CUT=1 leaves the cutting to ghost.framed (geo-tiles,
# road-tiles, in the background on a running box) instead of doing it here.
DEST="${1:?usage: fetch_geo.sh <dest-dir>}"
FORCE="${GHOST_GEO_REFRESH:-}"
NOCUT="${GHOST_GEO_NO_CUT:-}"
mkdir -p "$DEST"
CHANGED="$DEST/.fetch-geo-changed"
: > "$CHANGED"
changed() { echo "$1" >> "$CHANGED"; }
GN="https://download.geonames.org/export/dump"
# 10m, not 110m: at 110m Vancouver Island is a twelve-vertex cartoon and photo dots sit "in the
# ocean" next to a coastline that is the thing that is wrong. 24MB buys real fjords.
NE="https://raw.githubusercontent.com/nvkelso/natural-earth-vector/master/geojson/ne_10m_admin_0_countries.geojson"

get() { # get <url> <outfile>
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --retry 2 -o "$2" "$1"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O "$2" "$1"
    else
        echo "  note: neither curl nor wget present , skipping $(basename "$2")"
        return 1
    fi
}

# THE MIRROR FIRST. https://www.localghost.ai/mirror (that page says what it is and what a box
# promises) carries these same files, byte for byte as upstream publishes them, under a sha256
# manifest signed by the site key. tools/mirror_fetch.sh checks the signature against
# tools/mirror-key.asc (committed in this repo, pinned by fingerprint), then every file's hash, before
# anything lands here: a web host that was broken into can make it fail, never make it install
# something else. What the mirror does not deliver (unreachable, or a set not published yet) is SAID
# and left out: a box never takes a file that was not in a signed manifest. The upstream downloads
# below run only with GHOST_MIRROR_UPSTREAM=1, the operator's explicit, loud exception. Each set
# (geo, landpolygons, roads) stands alone: one missing does not stop the others.
# GHOST_MIRROR=<url> points at another copy (a LAN mirror, or file:///media/usb/mirror).
HERE="$(cd "$(dirname "$0")" && pwd)"
UPSTREAM="${GHOST_MIRROR_UPSTREAM:-}"
# why_not <rc> <set> , the one line that says why a set did not come from the mirror
why_not() {
    case "$1" in
        3) echo "  geo: set $2 is not on the mirror (not published yet, or GHOST_MIRROR=off; the line above says which)" ;;
        *) echo "  geo: set $2 could not be taken from the mirror (the lines above say why; a rerun resumes)" ;;
    esac
    if [ "$UPSTREAM" = 1 ]; then
        echo "  geo: !! GHOST_MIRROR_UPSTREAM=1: taking $2 from its upstream instead , NOT checked against a signed manifest"
    else
        echo "        nothing taken from the upstreams (GHOST_MIRROR_UPSTREAM=1 would, on your own authority)"
    fi
}
FETCH="$HERE/mirror_fetch.sh"
TILES="${GHOST_TILES_DIR:-$(dirname "$DEST")/landtiles}"
CUT="${GHOST_LANDTILES:-$HERE/../bin/ghost-landtiles}"
MIRROR_GEO=0
geo_missing() {
    for f in admin1CodesASCII.txt admin2Codes.txt countryInfo.txt allCountries.txt world.geojson world-50m.geojson world-110m.geojson; do
        [ -s "$DEST/$f" ] || return 0
    done
    return 1
}
# behind: installed from the mirror (a record) and the mirror now lists other bytes. The mirror not
# answering is not "behind": what is here stays.
listed() { sh "$FETCH" --list "$1" 2>/dev/null; }
behind() { # behind <set> <record file>
    [ -s "$2" ] || return 1
    _l="$(listed "$1")" || return 1
    [ -n "$_l" ] && [ "$_l" != "$(cat "$2")" ]
}
rc=0
if [ -z "$FORCE" ] && ! geo_missing && [ ! -s "$DEST/.mirror-geo" ]; then
    echo "  geo: present, not from a recorded mirror build , kept (GHOST_GEO_REFRESH=1 takes the mirror's)"
fi
if [ -n "$FORCE" ] || geo_missing || behind geo "$DEST/.mirror-geo"; then
    STAGE="$DEST/.mirror-dl"
    sh "$FETCH" geo "$STAGE"
    rc=$?
    # whatever arrived verified goes in (a file that failed is only a hidden .part, left to resume)
    for f in "$STAGE"/*; do
        [ -f "$f" ] || continue
        case "$f" in
            *.zip) command -v unzip >/dev/null 2>&1 && unzip -q -o "$f" -d "$DEST" && rm -f "$f" ;;
            *)     mv -f "$f" "$DEST/" ;;
        esac
    done
    case "$rc" in
        0) MIRROR_GEO=1
           rm -rf "$STAGE"
           listed geo > "$DEST/.mirror-geo.tmp" && mv -f "$DEST/.mirror-geo.tmp" "$DEST/.mirror-geo"
           changed geo
           echo "  geo: GeoNames + Natural Earth from the mirror, signature and hashes checked (terms beside them)" ;;
        3) rm -rf "$STAGE"; why_not 3 geo ;;
        *) why_not "$rc" geo ;;
    esac
else
    MIRROR_GEO=1   # all here already
fi

if [ "$MIRROR_GEO" = 0 ] && [ "$UPSTREAM" = 1 ]; then
    for f in admin1CodesASCII.txt admin2Codes.txt countryInfo.txt; do
        if [ -s "$DEST/$f" ] && [ -z "$FORCE" ]; then
            echo "  geo: $f already present (GHOST_GEO_REFRESH=1 to re-fetch)"
        elif get "$GN/$f" "$DEST/$f"; then
            echo "  geo: fetched $f"
        else
            echo "  note: could not fetch $f , place names will use raw codes until it is provided"
        fi
    done

    if [ -s "$DEST/allCountries.txt" ] && [ -z "$FORCE" ]; then
        echo "  geo: allCountries.txt already present (GHOST_GEO_REFRESH=1 to re-fetch)"
    elif command -v unzip >/dev/null 2>&1 && get "$GN/allCountries.zip" "$DEST/allCountries.zip"; then
        unzip -q -o "$DEST/allCountries.zip" -d "$DEST" && rm -f "$DEST/allCountries.zip"
        echo "  geo: fetched + unpacked allCountries.txt ($(du -h "$DEST/allCountries.txt" 2>/dev/null | cut -f1))"
    else
        echo "  note: could not fetch allCountries.zip (or unzip missing) , geocoding stays off until"
        echo "        the operator drops GeoNames TSVs in $DEST and runs geo-import"
    fi

    if [ -s "$DEST/world.geojson" ] && [ -z "$FORCE" ]; then
        echo "  geo: world.geojson already present"
    elif get "$NE" "$DEST/world.geojson"; then
        echo "  geo: fetched Natural Earth world.geojson"
    else
        echo "  note: could not fetch world.geojson , the MAP draws graticule + dots without landmass"
    fi

    # THE COARSE CUTS. The 10m file is the truth for a zoomed-in coastline and 24MB of truth is the wrong
    # thing to hand a phone before it can draw anything. Natural Earth publishes the same countries at
    # 110m (~800KB) and 50m (~4.5MB); the box serves whatever world*.geojson it has
    # (/v1/geo/world/index), the app opens on the smallest and refines with the largest once it is
    # cached. Existing boxes: drop these two files in <volume>/geo through ns.sh, nothing to restart.
    for res in 110m 50m; do
        url="https://raw.githubusercontent.com/nvkelso/natural-earth-vector/master/geojson/ne_${res}_admin_0_countries.geojson"
        if [ -s "$DEST/world-$res.geojson" ] && [ -z "$FORCE" ]; then
            echo "  geo: world-$res.geojson already present"
        elif get "$url" "$DEST/world-$res.geojson"; then
            echo "  geo: fetched Natural Earth world-$res.geojson"
        else
            echo "  note: could not fetch world-$res.geojson , the map opens on the full-detail file (slower first draw)"
        fi
    done
fi

# THE ROADS. OpenStreetMap's roads, Geofabrik's continent extracts (set `roads` on the mirror; ODbL), cut on the box into the map's road tiles , major roads in
# one-degree cells, every road with its name in tenth-of-a-degree cells , by bin/ghost-roadtiles
# here, or by ghost.framed at its next start (also: ghost-cli ghost.framed road-tiles). The mirror
# carries the continents it was asked to (roads are published by hand); Geofabrik only with
# GHOST_MIRROR_UPSTREAM=1. The whole
# world is about 70 GB of PBF and hours of cutting, so it is ASKED FOR, not assumed:
#   GHOST_GEO_ROADS=all                                        every continent
#   GHOST_GEO_ROADS="europe-latest.osm.pbf asia-latest.osm.pbf"   these files
# On a running box: sudo GHOST_GEO_ROADS=all ./tools/ns.sh ./tools/fetch_geo.sh <mount>/geo
# The PBFs stay under <geo>/roads so a newer extract can be cut again later.
ROADS="$DEST/roads"
RTILES="${GHOST_ROADTILES_DIR:-$(dirname "$DEST")/roadtiles}"
RCUT="${GHOST_ROADTILES:-$HERE/../bin/ghost-roadtiles}"
GEOFABRIK="https://download.geofabrik.de"
ALLROADS="africa-latest.osm.pbf antarctica-latest.osm.pbf asia-latest.osm.pbf australia-oceania-latest.osm.pbf central-america-latest.osm.pbf europe-latest.osm.pbf north-america-latest.osm.pbf south-america-latest.osm.pbf"
ROADFILES="${GHOST_GEO_ROADS:-}"
[ "$ROADFILES" = all ] && ROADFILES="$ALLROADS"
# not asked: the extracts this box already took from the mirror are kept current (a new extract on
# the mirror is fetched and cut again); nothing new is added without asking
if [ -z "$ROADFILES" ] && [ -d "$ROADS" ]; then
    for p in "$ROADS"/*.osm.pbf; do
        [ -f "$p" ] && [ -f "$ROADS/.$(basename "$p").sha256" ] && ROADFILES="$ROADFILES $(basename "$p")"
    done
    ROADFILES="${ROADFILES# }"
fi
if [ -z "$ROADFILES" ]; then
    if [ -s "$RTILES/index.bin" ]; then
        echo "  geo: road tiles present in $RTILES (GHOST_GEO_ROADS=all to refresh the extracts)"
    else
        echo "  geo: roads not fetched (GHOST_GEO_ROADS=all for the world's streets: ~70 GB and hours of cutting)"
    fi
else
    mkdir -p "$ROADS"
    got=0; missing=""
    for f in $ROADFILES; do
        if [ -s "$ROADS/$f" ] && [ -z "$FORCE" ] && [ ! -f "$ROADS/.$f.sha256" ]; then
            echo "  geo: $f present, not from a recorded mirror build , kept (GHOST_GEO_REFRESH=1 takes the mirror's)"
            got=$((got + 1))
            continue
        fi
        # from the mirror: a file whose record matches the current build costs one manifest read
        before="$(cat "$ROADS/.$f.sha256" 2>/dev/null)"
        frc=0; sh "$FETCH" roads "$ROADS" "$f" || frc=$?
        if [ "$frc" = 0 ] && [ -s "$ROADS/$f" ]; then
            got=$((got + 1))
            if [ "$(cat "$ROADS/.$f.sha256" 2>/dev/null)" != "$before" ]; then
                changed roads
                echo "  geo: $f from the mirror, signature and hash checked"
            else
                echo "  geo: $f current with the mirror"
            fi
            continue
        fi
        if [ -s "$ROADS/$f" ]; then
            why_not "$frc" "roads ($f)"
            echo "        the $f already here is kept"
            got=$((got + 1))
            continue
        fi
        why_not "$frc" "roads ($f)"
        if [ "$UPSTREAM" = 1 ] && curl -fL --proto-redir =https --retry 3 --retry-delay 5 -C - --progress-bar -o "$ROADS/.$f.part" "$GEOFABRIK/$f" && mv -f "$ROADS/.$f.part" "$ROADS/$f"; then
            got=$((got + 1))
            echo "  geo: $f from Geofabrik (GHOST_MIRROR_UPSTREAM=1)"
        else
            missing="$missing $f"
        fi
    done
    [ -n "$missing" ] && echo "  note: not fetched:$missing , those regions will have no streets until they are"
    if [ "$got" -gt 0 ]; then
        if [ -z "$FORCE" ] && [ -s "$RTILES/index.bin" ] && [ -d "$RTILES/graph" ] && [ "$(find "$ROADS" -name '*.osm.pbf' -newer "$RTILES/index.bin" | wc -l)" = 0 ]; then
            echo "  geo: the road tiles are already here ($RTILES)"
        elif [ -n "$NOCUT" ]; then
            changed roadtiles
            echo "  geo: the road tiles want cutting , left to ghost.framed (road-tiles, in the background)"
        elif [ -x "$RCUT" ]; then
            echo "  geo: cutting the roads into tiles (hours for the world; ~2 GB of RAM plus the page cache)"
            if "$RCUT" -out "$RTILES" -work "$RTILES.work" -in "$ROADS"; then
                cp "$ROADS"/TERMS-*.txt "$ROADS"/NOTICE.txt "$RTILES/" 2>/dev/null || true
                echo "  geo: road tiles in $RTILES"
            else
                echo "  note: the road cut failed , ghost.framed cuts at its next start (or: ghost-cli ghost.framed road-tiles)"
            fi
        else
            echo "  note: no bin/ghost-roadtiles (make box builds it) , ghost.framed cuts the road tiles at its next start"
        fi
    fi
fi

# THE COASTLINE AT FULL DETAIL. Natural Earth's 10m file is the base for the world and the
# continents; zoomed in on an island it is a smudge (Paxos is a handful of vertices). OpenStreetMap's
# land polygons draw every cove. The mirror carries OpenStreetMap's zip as it is (set landpolygons;
# osmdata.openstreetmap.de only with GHOST_MIRROR_UPSTREAM=1), and the BOX cuts it into one-degree tiles the phone fetches only under its
# viewport: here, with bin/ghost-landtiles (make box builds it), or else ghost.framed at its next
# start (it cuts whenever the shapefile is newer than the tiles; `ghost-cli ghost.framed geo-tiles`
# asks now). Several hundred MB, a few minutes and a couple of GB of RAM, once. ODbL: the map credits
# "© OpenStreetMap contributors" wherever it draws them. GHOST_GEO_NO_OSM=1 skips all of it.
OSM="https://osmdata.openstreetmap.de/download/land-polygons-complete-4326.zip"
SHPDIR="$DEST/land-polygons-complete-4326"
SHP="$SHPDIR/land_polygons.shp"
if [ -n "${GHOST_GEO_NO_OSM:-}" ]; then
    echo "  geo: OpenStreetMap land polygons skipped (GHOST_GEO_NO_OSM set) , the map keeps the 10m coast"
elif [ -z "$FORCE" ] && [ -s "$TILES/index.bin" ] && ! behind landpolygons "$SHPDIR/.mirror-landpolygons"; then
    if [ -s "$SHPDIR/.mirror-landpolygons" ]; then
        echo "  geo: the coastline tiles are here and current with the mirror ($TILES)"
    else
        echo "  geo: the coastline tiles are here, not from a recorded mirror build , kept (GHOST_GEO_REFRESH=1 takes the mirror's)"
    fi
else
    if [ -s "$SHP" ] && [ -z "$FORCE" ] && ! behind landpolygons "$SHPDIR/.mirror-landpolygons"; then
        echo "  geo: OpenStreetMap land polygons already present"
    else
        got=0
        LST="$DEST/.mirror-lp"
        command -v unzip >/dev/null 2>&1 || echo "  note: unzip is not installed (apt-get install unzip) , the land polygons cannot be unpacked"
        lrc=0; sh "$FETCH" landpolygons "$LST" || lrc=$?
        if [ "$lrc" = 0 ] && command -v unzip >/dev/null 2>&1 &&
           [ -s "$LST/land-polygons-complete-4326.zip" ] &&
           unzip -q -o "$LST/land-polygons-complete-4326.zip" -d "$DEST"; then
            cp "$LST"/TERMS-*.txt "$LST"/NOTICE.txt "$SHPDIR/" 2>/dev/null || true
            rm -rf "$LST"
            listed landpolygons > "$SHPDIR/.mirror-landpolygons.tmp" && mv -f "$SHPDIR/.mirror-landpolygons.tmp" "$SHPDIR/.mirror-landpolygons"
            changed landpolygons
            got=1
            echo "  geo: OpenStreetMap land polygons from the mirror, signature and hash checked ($(du -sh "$SHPDIR" 2>/dev/null | cut -f1))"
        fi
        [ "$got" = 0 ] && [ "$lrc" != 0 ] && why_not "$lrc" landpolygons
        if [ "$got" = 0 ] && [ "$UPSTREAM" = 1 ]; then
            if command -v unzip >/dev/null 2>&1 && get "$OSM" "$DEST/land-polygons-complete-4326.zip"; then
                unzip -q -o "$DEST/land-polygons-complete-4326.zip" -d "$DEST" && rm -f "$DEST/land-polygons-complete-4326.zip"
                rm -rf "$LST"
                echo "  geo: fetched + unpacked OpenStreetMap land polygons from upstream ($(du -sh "$SHPDIR" 2>/dev/null | cut -f1))"
            else
                echo "  note: could not fetch the OpenStreetMap land polygons , the map keeps the 10m coast when zoomed in"
            fi
        fi
    fi
    if [ -s "$SHP" ]; then
        if [ -n "$NOCUT" ]; then
            changed landtiles
            echo "  geo: the coastline tiles want cutting , left to ghost.framed (geo-tiles, in the background)"
        elif [ -x "$CUT" ]; then
            echo "  geo: cutting the coastline into tiles (a few minutes, a couple of GB of RAM)"
            if "$CUT" "$SHP" "$TILES"; then
                cp "$SHPDIR"/TERMS-*.txt "$SHPDIR"/NOTICE.txt "$TILES/" 2>/dev/null || true
                echo "  geo: coastline tiles in $TILES"
            else
                echo "  note: the cut failed , ghost.framed cuts it at its next start (or: ghost-cli ghost.framed geo-tiles)"
            fi
        else
            echo "  note: no bin/ghost-landtiles (make box builds it) , ghost.framed cuts the tiles at its next start"
            echo "        (or now: ghost-cli ghost.framed geo-tiles)"
        fi
    fi
fi
