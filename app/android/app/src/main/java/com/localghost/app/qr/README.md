# qr , reading the enrolment code from scratch

A QR decoder written by hand, no library. The box has a from-scratch QR encoder, and this is its
inverse, restricted to exactly the subset the enrolment QR uses (byte mode, versions one to ten, error
correction level M). Writing it rather than pulling a dependency keeps the app's dependency list near
zero and means the whole enrolment path, both ends, is code we own and can verify.

The pipeline has two halves. QrSampler is the image-processing half, taking a grayscale frame and
turning it into a grid of dark and light modules by binarising, finding the three finder patterns, and
sampling each module's centre. QrMatrixDecode is the data half, reading that grid back to bytes.
GaloisField and ReedSolomon are the error correction below them, ported faithfully from a validated
reference and covered by round-trip tests that check it corrects up to capacity and rejects beyond it
rather than mis-correcting.

The decoder is solid and tested. The known bug here is not in the decoding, it is in the surface, the
scanner stays silent when a code is bad or unreadable so the person has no idea whether it is working.
That is a UX fix in the screen, not a decoder change.
