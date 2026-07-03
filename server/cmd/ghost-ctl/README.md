# cmd/ghost-ctl

A command-line client, mostly for testing the box without the phone app. Its first job is enrolment:
given an enrol link it saves the device cert + key the link itself carries (no network call, no pairing
exchange , the link IS the credential), after which it can talk to ghost.secd like any other enrolled
device. Handy when you want to poke the box surface directly and watch what comes back.
