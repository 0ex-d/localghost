# internal/auth

The job here is to make guessing expensive and to make every guess look the same. Rate-limiting,
lockout, and a constant-time credential check. The hardware-backed key seal is a seam this package
defines and hw implements.

The lockout counts failures and backs off, and it treats a wrong PIN and a wipe-PIN attempt the same
way, so the gate itself gives nothing away about which kind of PIN just failed. The credential check is
constant-time Argon2id, so the duration of a check does not tell an attacker they are getting warmer.

The thread running through all of it is uniformity. An onlooker, or a timing attacker, should not be
able to tell a right PIN from a wrong one from a wipe one by how the check behaves, only by the outcome,
and the appear-down layer is there to hide the outcome too.
