# Tonight's runbook , deploy + test, in order

## Deploy
1. Server: unzip dbfix.zip over the repo, then
   `sudo GHOST_PIN=<pin> ./tools/redeploy.sh`   (rides the graceful halt, swaps binaries)
2. Unlock from the app (install the new APK first , everything app-side is in localghost-app.zip)
3. Geo (xyntai one-time): drop allCountries.txt + admin1CodesASCII.txt + admin2Codes.txt +
   countryInfo.txt + world.geojson into <volume>/geo via ns.sh, then
   `ghost-cli ghost.framed geo-import`   (watch framed's log for "geo import done")
4. `ghost-cli ghost.framed reprocess force=true`  (places + upright thumbs + journals the archive)
5. `ghost-cli ghost.searchd unpark`

## Test, roughly in dependency order
- CHAT: markdown renders, thinking expands at brief/deep (the box-side splitter), copy works,
  keyboard behaves, continuity across restart
- GALLERY: search "waterfall" style queries, place line in detail, tags add/remove
- MAP: landmass + dots + tap-for-place; day tracks appear once GPS/timeline data exists
- MEMORIES: check-in (chips, prefilled why), jot a note, add/edit/delete a memory,
  ON THIS DAY (first tap of a day takes a minute)
- Have one chat, wait 10 quiet minutes: it appears as journal -> distilled memory; delete the
  memory, confirm it stays deleted after the next pass
- Share text from another app -> toast -> journal
- SYNC: permission rows with rationales, CONNECT HEALTH -> grant -> SYNC HEALTH (7 DAYS)
- HEALTH screen: bars appear after the sync
- Timeline (whenever the Takeout finishes): drop the JSON in <volume>/framed/gps-inbox,
  `ghost-cli ghost.framed gps-import`, then admire the MAP
- Box Status: stale banner only if you kill watchd by hand; killing it should also
  produce a notification + a respawn within ~15s (item 24 exercising)

## Known allowances
- Health Connect gradle dep pinned to alpha07 , adjust if the build objects
- OTD groups by UTC day (late-evening local photos can land a day over)
- First OTD/narrative calls are model-speed; everything cached after
