# Contributing

Thanks for looking. This is a maintained fork of
[ribbybibby/s3_exporter](https://github.com/ribbybibby/s3_exporter), whose
upstream has not released since 2021. Street Fortress Industries runs it in
production, so changes are welcome and reviewed.

## Where the code lives

The repository has two homes, and they are not equal:

- **`gitea.zen.lofi/oss/s3-exporter` is the source of truth.** It is Street
  Fortress's own gitea. It is not reachable from the internet.
- **`github.com/streetfortress/s3-exporter` is the published copy.** A job
  inside Street Fortress pushes `main` and the `v*` tags to it. Nothing is
  merged on GitHub, because a merge there would make the two `main` branches
  disagree and stop the publishing.

This is the part to know before you spend time on a change: **a maintainer
has to carry your patch across.** GitHub is where you and the maintainers
talk, and it is not where the commit lands.

## Reporting a bug or asking for a change

Open an issue on GitHub:
<https://github.com/streetfortress/s3-exporter/issues>

For a bug, please include the version (`s3_exporter --version`), the S3
store or API in front of it (Backblaze B2, MinIO, AWS S3, RustFS, …), the
flags or environment you ran it with, the request you made, and the
response or the log line you got. A probe that answers
`s3_list_success 0` is the exporter working as designed — say what you
expected instead.

For a security problem, do not open an issue. [SECURITY.md](SECURITY.md)
has the private channel.

## Sending a change

1. Open an issue first for anything beyond a small fix, so nobody writes
   the same patch twice.
2. Open a pull request against `main` on GitHub. A maintainer reviews it
   there.
3. On approval, a maintainer applies your commits in gitea, keeping you as
   the author, and closes the GitHub pull request with a link. Your commit
   appears on GitHub when `main` is next published.

A patch by email or attached to an issue is fine too, if a pull request is
inconvenient.

## What a change has to keep

**The metric names and the `/probe` contract are upstream's.** People run
this fork as a drop-in replacement, and their dashboards and alert rules
name `s3_objects`, `s3_last_modified_object_date` and the rest. A change to
a metric name, a label, or the shape of a `/probe` response breaks them.
Such a change needs a strong reason, and it makes the next release a major
one.

**A failed listing is a metric, not an error.** `/probe` answers 200 with
`s3_list_success 0` when the listing fails. That sample is the point:
alerting rules key on it, and every other series is meaningless while it is
0. Do not turn a failure into a non-200 or a panic.

**No probe fetches an object.** The exporter lists. The credential an
operator gives it is not allowed to read object content, and the code must
not need more.

## Running the checks

[`just`](https://github.com/casey/just) drives the repository. Go comes
from `go.mod`; Helm 4 is needed for the chart recipes.

```
just check      # go vet + go test, helm lint + helm template
just image      # docker build → s3-exporter:dev
just run https://s3.us-west-004.backblazeb2.com us-west-004
                # runs the dev image with AWS_* from the environment, then:
                # curl 'localhost:9340/probe?target=BUCKET/PREFIX/'
```

`just check` is what CI runs on every push. Run it before you send a
change. A change to the probe logic wants a test in `s3_exporter_test.go`;
a change to the chart wants `helm template` output in the pull request.

## Style

- Go as `gofmt` writes it. No lint config beyond `go vet`.
- Keep the comments that say *why*. This repository puts the reasoning in
  the file it belongs to — the version stamping in `Chart.yaml`, the
  registry paths in `.github/workflows/release.yml` — and a change that
  makes such a comment wrong has to fix it.
- One logical change per pull request.

## Licence

Apache License 2.0, as upstream ([LICENSE](LICENSE)). By sending a change
you agree to publish it under that licence. There is no CLA.
