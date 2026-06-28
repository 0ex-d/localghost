# local , the on-phone lifeboat

A model that runs on the phone itself, for when the box cannot be reached. It is the lifeboat, not the
engine. It has no access to the life-index (that lives on the box), so it answers generic questions with
whatever limited context it has, and the moment the box is reachable again the real model takes over.

The reason it exists at all is that a privacy product that goes dark the instant your home connection
drops is not much of a product. So the phone carries a fallback. It is used automatically when the box
is unreachable, or manually when the person forces local mode.

LocalModel is the swappable seam, same idea as BoxClient, so the rest of the app does not know it is
talking to llama.cpp below it. NativeLlama is the raw JNI binding, one to one with the C++.
ModelStore keeps models in the app's private storage, never bundled and never leaving the device.
ModelDownloadWorker pulls a model from the box as a resumable foreground job. ModelVerifier is the
integrity check, and it matters in both directions, because accepting a corrupted model is bad and
rejecting a good one loops forever re-downloading, so its SHA-256 check against the box-published hash
has its own unit test.
