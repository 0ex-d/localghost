# internal/wipe

Destruction here means destroying the key, not scrubbing the data. You cannot reliably overwrite a
modern SSD (wear levelling and over-provisioning mean the bytes you think you erased may still be sitting
in a spare block), so this package does not pretend to. It makes the data unreadable by throwing away
the only thing that can decrypt it.

The disk is locked by the AMK, and the AMK is sealed in the TPM. The wipe evicts that sealed key. After
that the LUKS keyslot derived from it is meaningless and the 32 random bytes that were the key are gone
from the one place they lived. This is what lets the wipe defend against an attacker who imaged the disk
last month and gets a PIN out of you today. The ciphertext they captured is now permanently locked,
because the key it needed no longer exists anywhere.

The wipe PIN triggers the global version, which burns everything. Keys are held in mlock'd buffers so
they never hit swap, and they are zeroised on wipe and on unmount.

The actual eviction is a seam, wired in the real build to the TPM. If it is not wired, the wipe reports
itself as best-effort rather than claiming a clean erase it cannot guarantee, which matters, because the
one thing worse than a wipe that fails is a wipe that says it worked when it did not.
