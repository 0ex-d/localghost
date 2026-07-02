# cmd/ghost-setup

The thing you run once to provision a box, and the part of the system most likely to do something you
cannot undo, so it is built to make that hard to do by accident.

Run it with no --disk or --host and it becomes a wizard. It lists the disks with their sizes and flags
the ones that are mounted or already in use, so you pick the empty one from a menu instead of typing a
device path and praying you did not fat-finger nvme1n1 into nvme0n1. It asks for the host, takes the PINs
with the echo off so they never hit your screen or your shell history, shows you what it is about to
erase, and makes you type it out before it touches anything. The flags still work for scripted runs.

PINs are prompted, never passed as flags, because a flag would put the PIN in the process list and the
shell history where anyone could read it. It refuses to re-provision a disk that is already set up. And
it needs the `-tags tpm` build to seal a key and format, because there is no honest way to
provision encrypted storage without the TPM. The real work is in internal/setup and internal/setup/debian.
