# internal/zstd

A copy of the Go standard library's own zstd decompressor (`$GOROOT/src/internal/zstd`, Go 1.25.1),
unchanged, under Go's BSD licence (LICENSE beside it). Go keeps it internal (debug/elf reads
compressed sections with it), so a program outside the standard library cannot import it; the box
needs it to read the Wikipedia copy (internal/zim: Kiwix's ZIM files compress their clusters with
zstd). Copied rather than taken from a third-party module: the code is the Go authors', fuzzed and
tested with Go itself.

To refresh it from a newer Go: copy bits.go, block.go, fse.go, huff.go, literals.go, window.go,
xxhash.go, zstd.go, the tests that need neither an internal package nor a zstd or xxhsum
command (fse_test.go, window_test.go) and testdata/ again. `zstd_files_test.go` is ours.
