# Security Policy

## Supported versions

The latest `vX.Y.Z` release is the only supported one. Fixes land on `main`
and ship in the next tag; there are no maintenance branches.

## Reporting a vulnerability

Report privately, through GitHub's private vulnerability reporting:

**<https://github.com/streetfortress/s3-exporter/security/advisories/new>**

Please do not open a public issue for a vulnerability. If private reporting
is unavailable to you, open a public issue that says only "security report"
and asks for a private channel — no details — and a maintainer will answer
with one.

This is a small fork maintained by Street Fortress Industries alongside
other work. Expect an acknowledgement within a week. There is no bounty
programme.

Useful in a report: the version, the S3 store or API the exporter talked
to, the request that triggered the behaviour, and what an attacker gains.

## What this exporter touches

Two facts bound most of the threat model.

**The credential is read-only, and the exporter only lists.** It reads
`AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` from its environment and
makes `ListObjectsV2` and `ListBuckets` calls. It never fetches an object,
and the recommended credential cannot: `s3:ListBucket` alone on an
S3-compatible store, `listBuckets` + `listFiles` and no `readFiles` on
Backblaze B2. A leaked exporter credential reveals which objects exist and
when they changed, never their content.

**The HTTP surface is unauthenticated on purpose.** `/probe`, `/discovery`
and `/metrics` have no authentication and no TLS, like every Prometheus
exporter. Anyone who reaches the port can ask the exporter to list any
bucket and prefix the credential can reach, and can read the object counts,
sizes and timestamps that come back. Treat the port as internal: a
ClusterIP Service and a NetworkPolicy, or a host that only Prometheus
reaches. An exporter exposed to the internet is a misconfiguration, not a
vulnerability in this code.

Reports we do want, as examples: the credential appearing in a log line, a
response body or a metric label; a request that makes the exporter fetch
object content; a path that reaches an S3 endpoint the operator did not
configure; a crash or an unbounded allocation from a crafted S3 response.

## Dependencies

A vulnerability in a dependency — the AWS SDK, `prometheus/client_golang` —
belongs to that project. Report it there. Tell us as well if this exporter
needs a change beyond a version bump, and we will cut a release once the
fixed version is out.
