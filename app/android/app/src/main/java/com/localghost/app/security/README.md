# security , identity, the lock, and the setup signal

The phone's own secrets and the rules about when it is allowed to be open. None of this is the box's
PIN (that lives on the box); this is the layer that decides whether the person holding the phone right
now gets to use it, and that keeps the phone's identity key somewhere a thief cannot extract it.

DeviceCert holds the box-issued device certificate + private key delivered in the enrolment QR (the
the hardware allows. It signs the CSR the box enrols and authenticates the mTLS channel, and the
private key never leaves secure hardware. AppLock is the biometric gate, a keystore key bound to user
authentication so the fingerprint or face prompt proves a real person and not a tapped button.
BoxConfig is the encrypted store for the box connection details written at enrolment, and its
empty-versus-present state is how the app knows whether it is in setup or in use.

AuthGate is the small, fiddly, bug-prone one, and it was pulled out of MainActivity precisely so it
could be tested without a device. It is the lock-on-background decision, and the bugs it exists to
prevent are the ones where an in-app picker or a rotation backgrounds the app and wrongly relocks it.
One of the two known app bugs lives near here, a fingerprint re-prompt that fires mid-setup after the
app goes to the background, which it should not.
