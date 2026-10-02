# Releases

Each release has a name (`server/tools/release.names`), its notes (`server/releases/<version>.md`:
what it does, what is in it, how it works) and a pin (`server/releases/pins.txt`: the commit it
was cut from). `server/tools/cut_release.sh <version>` cuts it, or cuts it again from the same
commit, to the same bytes.

| version | name | date | notes |
|---|---|---|---|
| 0.0.1 | wisp | 2 October 2026 | [server/releases/0.0.1.md](server/releases/0.0.1.md) |
