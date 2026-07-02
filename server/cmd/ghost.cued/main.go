// ghost.cued is the cueing daemon: it decides when to put a small, timely question in front of you
// , keep this photo, surface this memory , and produces it as an ANSWERABLE notification (an "ask"
// with options), which the app renders as buttons and posts back to /v1/notifications/answer.
//
// HONEST STATE. The hard part of ghost.cued is the trigger: reading context and deciding WHAT to
// cue and WHEN, quietly and rarely (see the "Before You Ask" Hard Truth). That depends on
// ghost.synthd (which surfaces the memory) and, for the shoebox nomination, on the photo pipeline
// that scores the day's best few. Neither is built yet, so this binary does not fake that judgement.
// What it does provide, for real and end to end, is:
//
//   - the producer plumbing: it writes ghost.cued asks through the SAME NotifStore.Produce path the
//     real daemon will use (Postgres history + Redis push cache), not a shortcut;
//   - one concrete, settled cue , the shoebox nomination , raisable by hand so the ask/answer loop
//     can be exercised against the app today: `ghost.cued nominate -photo "beach, this morning"`
//     produces a "Keep this one?" ask with Keep/Skip, which you answer from the phone.
//
// When ghost.synthd and the photo scorer exist, the automatic trigger replaces the manual nominate
// subcommand and calls the same produce path. Until then this is the real home for ghost.cued, not a
// stub that pretends to think.
//
// The account must be UNLOCKED (volume mounted, Postgres + Redis up) when this runs; it writes into
// the in-volume databases and does not unlock anything itself.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/LocalGhostDao/localghost/server/internal/hw"
)

// service is this daemon's notification identity. It must match the allow-list in secd
// (notificationServices) and the mute scopes, so a cue can be muted like any other service.
const service = "ghost.cued"

// keepOptions are the two answers a shoebox nomination offers. Keep sends the photo to the plain
// shoebox (survives a forgotten code); Skip leaves it in the encrypted tiers only.
var keepOptions = []string{"Keep", "Skip"}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "nominate":
		nominate(os.Args[2:])
	default:
		usage()
	}
}

// nominate produces one shoebox-nomination ask for a photo. This is the manual stand-in for the
// automatic daily selection until the photo scorer exists; the produced ask is real and answerable.
func nominate(args []string) {
	fs := flag.NewFlagSet("nominate", flag.ExitOnError)
	stateDir := fs.String("state", "/var/lib/ghost", "unencrypted state dir (to locate the mount)")
	slot := fs.Int("slot", 0, "account slot (single-account model: 0)")
	photo := fs.String("photo", "", "a short reference for the photo being nominated (required)")
	_ = fs.Parse(args)
	if *photo == "" {
		fmt.Fprintln(os.Stderr, "nominate: -photo is required")
		os.Exit(2)
	}

	store := hw.NewNotifStore(func(s int) string {
		mnt := filepath.Join(*stateDir, "mnt", fmt.Sprintf("slot%d", s))
		return hw.SocketForMount(mnt)
	})

	ask := hw.Notification{
		Service: service,
		Kind:    "ask",
		Title:   "Keep this one?",
		Body:    fmt.Sprintf("A favourite from today (%s) , keep it in the plain shoebox so a lost code can never erase it?", *photo),
		Options: keepOptions,
	}
	if err := store.Produce(*slot, ask); err != nil {
		fmt.Fprintf(os.Stderr, "produce cue: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("raised a shoebox nomination for %q into slot %d\n", *photo, *slot)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: ghost.cued nominate -photo <ref> [-state DIR] [-slot N]")
	fmt.Fprintln(os.Stderr, "  raises a real ghost.cued ask (the automatic trigger awaits ghost.synthd)")
	os.Exit(2)
}
