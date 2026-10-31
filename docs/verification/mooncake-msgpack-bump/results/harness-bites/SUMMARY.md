# Harness-bites proof (run 2026-11-01)

Two tampered copies of the PR-head requirements.txt were fed to
`scripts/verify_msgpack_lock.sh`; both contract claims are green on the real head
(see ../verdicts.tsv), so any red below is the harness biting.

## Bite A — corrupt the digest of a wheel the image platform never installs
(`07c9733089…` = the **cp313** manylinux x86_64 wheel's digest → `aa0000000a…`)

| Claim | Verdict | Reason |
|-------|---------|--------|
| C1 | FAIL | `EXTRA 1 hash(es) not on PyPI` + `MISSING 1 PyPI digest(s)` — set equality is broken |
| C2 | pass | correct, not a miss: py312/manylinux x86_64 resolution never selects the cp313 wheel, and that wheel's digest is still absent-from-lock only via C1. The lock remains installable, so C2 must stay green |

C1 is the claim that catches this class of tamper.

## Bite B — corrupt the digest of the wheel the image platform actually installs
(`ede33b2892…` = the **cp312** manylinux x86_64 wheel's digest → `bb1111111b…`)

| Claim | Verdict | Reason |
|-------|---------|--------|
| C1 | FAIL | EXTRA + MISSING as above |
| C2 | FAIL | pip's own verifier: `ERROR: THESE PACKAGES DO NOT MATCH THE HASHES FROM THE REQUIREMENTS FILE` (bite-B-c2-pip-download.log); the curl fallback independently shows the fetched artifact's digest differs from every lock entry |

Both claims red for the right reason on a broken lock, green on the PR head.
Production code untouched.
