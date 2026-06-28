# ui , the screens

Jetpack Compose, portrait, single activity. The screens that turn the rest of the app into something a
person uses. They hold no logic worth protecting, that is all in net, security, and the box, so this
package is about presenting state and collecting input cleanly.

The ones that carry the most weight are SetupScreen (enter the box address by typing or QR, the
one-time code, and a device name, then enrol), the PIN and Lock screens (the gate into a session),
NotificationsScreen (the list, and where seen and delete-forever will live), and the settings screens
including the mute controls. MainShell is the frame the rest sit in.

Two of the app's known issues surface in this package even though their causes sit elsewhere. The QR
scan screen needs a visible failure state when a code will not read, and the setup flow should survive
the app being backgrounded without a stray fingerprint re-prompt interrupting it. After today's server
changes, NotificationsScreen and the mute settings need the most work, the per-service plus global mute
UI, the list and seen and delete actions, and generic-versus-specific rendering off each
notification's kind.
