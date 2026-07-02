# internal/container

What is left of this package is the seam between "open the account" as an idea and the dm-crypt mount
that does it. One interface, the Mounter, and the real implementation living in hw.

It used to hold a lot more. The equal-size containers, the layout verifier, the keyless lockstep growth,
all of it existed to make several encrypted blobs look identical on disk so none of them gave away which
was real. That whole approach went when the decoys did. It was dead code by the end, used nowhere but
its own tests, so this package shrank to the one thing the system still needs, which is a clean boundary
between the unlock flow and the hardware that mounts the disk.

The account's container is the whole raw disk now. No partition table, no image file sitting in a
filesystem, no sizes to keep matched. There is one disk and it is either locked or open.

One leftover worth knowing about. ResizeToFill is still on the interface but it does nothing useful,
because the raw disk is formatted to its full size at setup, so there is never any slack to grow into.
It is harmless and it stays for now because the interface expects it.
