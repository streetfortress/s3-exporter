# s3-exporter

A probe-style Prometheus exporter for S3 buckets. One `ListObjects` per
probe, answering three questions about everything under a prefix: how
many objects, how big, and **when the newest one was written**.

SFI's fork of [ribbybibby/s3_exporter](https://github.com/ribbybibby/s3_exporter)
(unmaintained upstream; last release 2021). Metric names and the `/probe`
contract are upstream's; what changed is listed at the end. SFI runs it as
the bucket-side ground truth for backup freshness: every other backup
signal comes from the unit that claims to have written, and this asks the
bucket instead.

## Overview

### The probe model

Like blackbox_exporter, the exporter has no targets of its own. Prometheus
asks it to look at one bucket and prefix per scrape:

```
GET /probe?bucket=BUCKET&prefix=PREFIX[&delimiter=D]
GET /probe?target=BUCKET/PREFIX
```

The two forms are equivalent. `target=` exists because the
prometheus-operator `Probe` CR, and the usual blackbox relabelling in a
`ScrapeConfig`, pass a single parameter: the text up to the first `/` is
the bucket, the rest (which may be empty, and usually ends in `/`) is the
prefix. `bucket=` and `target=` together are a 400.

The exporter lists the prefix — every page of it — and answers with
`s3_objects`, `s3_objects_size_sum_bytes`, `s3_biggest_object_size_bytes`,
`s3_last_modified_object_date` (a Unix timestamp; the freshness) and
`s3_last_modified_object_size_bytes`, all labelled `bucket` and `prefix`,
plus `s3_list_success` and `s3_list_duration_seconds`. With `delimiter=`
it counts common prefixes instead (`s3_common_prefixes`) — "how many
top-level directories" — and reports none of the object metrics.

A listing that fails — a wrong or revoked key, the endpoint refusing, an
account over its caps — is **a metric, not an error**: the probe still
returns 200 with `s3_list_success 0`. That is the sample the failure case
exists to produce, and the one to alert on first, because every other
series is blind while it is 0. (Upstream panicked here instead.)

An **empty prefix** reports `s3_objects 0` and
`s3_last_modified_object_date` as the zero time (`-6.795364578e+09`), so
"age of newest object" is enormous rather than absent. For a prefix that
is *expected* to have content that is the right answer.

`/discovery` is an `http_sd` endpoint: one target per bucket the
credential can list, each carrying `__param_bucket`. It is the way to
get a probe of every bucket an S3 store holds without listing them by
hand — a fort-local store whose buckets are created by applications, say
— at the cost of probing whole buckets rather than prefixes. See
[Discovery](#discovery).

### Runtime expectations

Every probe is metadata only. **No object is ever fetched**, and the
recommended credential cannot fetch one: on B2 that is `listBuckets` +
`listFiles` and no `readFiles`; on an S3-compatible store, `s3:ListBucket`
alone. A leaked exporter credential reveals what exists and when, never
content.

What a probe costs:

- **Calls.** `ceil(objects / 1000)` list calls per probe — one for
  anything under a thousand objects. On Backblaze B2 these are **Class C
  transactions** (2,500/day free, then $0.004 per 1,000); they do not
  touch the download-bandwidth cap. On a self-hosted store they are CPU
  on that host.
- **Bytes.** The response is the listing: roughly 200 bytes per object
  (key, size, ETag, mtime). ~100 KB for a 500-object prefix. Not egress
  in any sense that bills.
- **Scale with object count, not bytes.** A restic repository is one
  pack per ~16–32 MB of unique data (a 400 GB repo ≈ 15,000 objects ≈ 15
  calls per probe); a WAL archive grows a segment at a time and only
  shrinks by lifecycle rule. If a prefix gets large, probe it with a
  `delimiter`, or probe a narrower prefix — not less often.

**Scrape interval:** at most Prometheus's staleness window (5 m by
default). A series scraped less often than that is invisible to instant
queries and to alert-rule evaluation between scrapes; alerts with a `for`
never hold. Seven targets every 5 m is ~2,000 B2 Class C calls a day.
Wrap rule expressions in `last_over_time(...[30m])` so one slow or timed
out listing does not reset an alert either.

## Installation

Helm is the primary method. The chart and the image are built from this
repository at the same tag and published together:

```
oci://gitea.zen.lofi/sfi/helm-s3-exporter     chart, version X.Y.Z
gitea.zen.lofi/sfi/s3-exporter:X.Y.Z          image
```

(Different names on purpose: an OCI registry keys artifacts by path and
tag regardless of type, and a same-named chart push replaces the image.)

One release per S3 store — the credential and the endpoint go together:

```yaml
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: s3-exporter-b2
  namespace: monitoring-central
spec:
  chartRef: {kind: OCIRepository, name: helm-s3-exporter, namespace: flux-system}
  values:
    fullnameOverride: s3-exporter-b2
    existingSecret: s3-exporter-b2-credentials    # AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY
    s3:
      endpointURL: https://s3.us-west-004.backblazeb2.com
      region: us-west-004                          # some stores want one even when the endpoint decides it
      forcePathStyle: true
```

Values of note: `existingSecret` (the chart never renders a Secret),
`s3.endpointURL` / `s3.region` / `s3.forcePathStyle`, `probe.*` (an
optional prometheus-operator `Probe` with static `bucket/prefix` targets,
for clusters whose Prometheus selects Probes), `extraArgs`, `resources`.
Chart `version` and `appVersion` are the same X.Y.Z, so the default image
is the one built from the same commit.

Also possible: `docker run gitea.zen.lofi/sfi/s3-exporter:X.Y.Z` with the
credential in the environment and the flags below, or `go build .` — the
binary is static and listens on `:9340`.

## Configuration

### Static targets (ScrapeConfig)

The shape SFI runs: a prometheus-operator `ScrapeConfig` whose static
targets are `bucket/prefix` strings, relabelled the way blackbox targets
are. The exporter labels every series with `bucket` and `prefix`; the
relabelling keeps `instance` = the target string.

```yaml
apiVersion: monitoring.coreos.com/v1alpha1
kind: ScrapeConfig
metadata:
  name: s3-b2
  namespace: monitoring-central
  labels:
    monitoring.sfi/scope: central
spec:
  jobName: s3_b2
  scrapeInterval: 5m          # ≤ the staleness window; see Runtime expectations
  scrapeTimeout: 2m
  metricsPath: /probe
  staticConfigs:
    - targets:
        - sfi-pv-backups/zenbook/
        - sfi-db-logical-backups/zenbook/
        - sfi-db-physical-backups/sfi-prod-439bf72/sites-db-v2/wals/
        - sfi-db-physical-backups/sfi-prod-439bf72/sites-db-v2/base/
  relabelings:
    - sourceLabels: [__address__]
      targetLabel: __param_target
    - sourceLabels: [__param_target]
      targetLabel: instance
    - targetLabel: __address__
      replacement: s3-exporter-b2.monitoring-central.svc.cluster.local:9340
```

The same in a plain `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: s3_b2
    scrape_interval: 5m
    metrics_path: /probe
    static_configs:
      - targets: [sfi-pv-backups/zenbook/, sfi-db-logical-backups/zenbook/]
    relabel_configs:
      - source_labels: [__address__]
        target_label: __param_target
      - source_labels: [__param_target]
        target_label: instance
      - target_label: __address__
        replacement: s3-exporter-b2:9340
```

Or the chart's `Probe` (`probe.enabled: true`, `probe.targets: [...]`),
for a Prometheus whose `probeSelector` picks it up — same targets, same
labels, the operator does the relabelling.

### Rules

What SFI alerts on, as a starting point:

```yaml
- alert: B2ListingFailing                # first, and critical: everything else is blind
  expr: last_over_time(s3_list_success{job="s3_b2"}[30m]) == 0
  for: 30m
- alert: B2WALStale                      # a WAL archive gets a segment at least every few minutes
  expr: >-
    time() - last_over_time(s3_last_modified_object_date{job="s3_b2", prefix=~".*/wals/"}[30m]) > 3600
      and on (instance) last_over_time(s3_list_success{job="s3_b2"}[30m]) == 1
  for: 15m
- alert: B2BackupStale                   # nightly at slowest; an empty prefix fires too
  expr: >-
    time() - last_over_time(s3_last_modified_object_date{job="s3_b2", prefix!~".*/wals/"}[30m]) > 36 * 3600
      and on (instance) last_over_time(s3_list_success{job="s3_b2"}[30m]) == 1
  for: 15m
```

Growth over time is `s3_objects_size_sum_bytes` on a dashboard;
`s3_list_duration_seconds` says when a prefix has grown enough to matter.

### Discovery

`/discovery` returns `http_sd` targets, one per bucket the credential can
list:

```yaml
scrape_configs:
  - job_name: s3_all_buckets
    metrics_path: /probe
    http_sd_configs:
      - url: http://s3-exporter-local:9340/discovery
    relabel_configs:
      - source_labels: [__param_bucket]
        target_label: instance
      - target_label: __address__
        replacement: s3-exporter-local:9340
```

Each target arrives with `__param_bucket` set and no prefix, so this
probes **whole buckets**. That fits a store whose buckets are the unit of
interest and are created by something other than you — a fort-local S3
where every application that asks for a bucket gets metrics without a
line of config anywhere. It does not fit a backup store, where the
question is per host per prefix inside a shared bucket; use static
targets there. The credential needs `listBuckets` for this endpoint.

### Flags

```
--web.listen-address=":9340"     Address to listen on for web interface and telemetry
--web.metrics-path="/metrics"    The exporter's own metrics
--web.probe-path="/probe"
--web.discovery-path="/discovery"
--s3.endpoint-url=""             Custom endpoint URL (any S3-compatible store)
--s3.region=""                   Region, for stores that want one (also AWS_REGION)
--s3.force-path-style            Bucket in the path, not the host — most self-hosted stores
--log.level=info
```

Every flag is also an environment variable prefixed `S3_EXPORTER_`
(`S3_EXPORTER_S3_ENDPOINT_URL=…`). Credentials come from the AWS SDK's
usual chain: `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` in the
environment is the one the chart uses.

### Metrics

| Metric | Meaning | Labels |
| --- | --- | --- |
| `s3_list_success` | Did the ListObjects operation complete? 0 on any failure — alert on this first | bucket, prefix, delimiter |
| `s3_list_duration_seconds` | Wall time of the listing, all pages | bucket, prefix, delimiter |
| `s3_last_modified_object_date` | Unix time of the most recently modified object; the zero time (−6.8e9) when the prefix is empty | bucket, prefix |
| `s3_last_modified_object_size_bytes` | Size of that object | bucket, prefix |
| `s3_objects` | Object count under the prefix | bucket, prefix |
| `s3_objects_size_sum_bytes` | Total size under the prefix | bucket, prefix |
| `s3_biggest_object_size_bytes` | Largest object under the prefix | bucket, prefix |
| `s3_common_prefixes` | With `delimiter=`: count of common prefixes (the object metrics are not reported) | bucket, prefix, delimiter |

## Development

```
just check      # go vet + go test, helm lint + template
just image      # docker build → s3-exporter:dev
just run https://s3.us-west-004.backblazeb2.com us-west-004
                # runs the dev image with AWS_* from your environment; then
                # curl 'localhost:9340/probe?target=BUCKET/PREFIX/'
just tag X.Y.Z  # tags HEAD (must be at origin/main) and pushes; CI builds
                # the image, then the chart, into the gitea registries
```

CI runs vet, tests, an image build and a chart lint on every push
(`.gitea/workflows/build.yaml`); releases are `.gitea/workflows/release.yaml`.

## What changed from upstream

- AWS SDK v2 and Go 1.26 (upstream: the deprecated v1 SDK, vendored, Go
  1.15); `prometheus/common/log` → `log/slog`; module renamed.
- `/probe?target=BUCKET/PREFIX`.
- A refused listing is `s3_list_success 0` with a 200, not a panic.
- `--s3.region`.
- Helm chart, `Containerfile` (static, scratch, non-root), gitea CI.
- Docker Hub publishing, promu, goreleaser and the Makefile removed.

Licensed under the Apache License 2.0, as upstream (`LICENSE`).
