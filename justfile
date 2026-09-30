# Local checks (mirror what CI runs on every push).
check:
    go vet ./... && go test ./...
    helm lint charts/s3-exporter
    helm template t charts/s3-exporter --set probe.enabled=true --set 'probe.targets={b/p/}' >/dev/null

# Build the image locally as s3-exporter:dev.
image:
    docker build -t s3-exporter:dev -f Containerfile .

# Run the local image against an endpoint, credentials from the environment:
#   AWS_ACCESS_KEY_ID=… AWS_SECRET_ACCESS_KEY=… just run https://s3.us-west-004.backblazeb2.com us-west-004
# then: curl 'localhost:9340/probe?target=BUCKET/PREFIX/'
run endpoint region="":
    docker run --rm -p 9340:9340 -e AWS_ACCESS_KEY_ID -e AWS_SECRET_ACCESS_KEY s3-exporter:dev \
        --s3.endpoint-url={{endpoint}} --s3.region={{region}} --s3.force-path-style

# Release
# Tag HEAD for release and push the tag to the internal gitea remote. The
# ref-pusher of sfi/deployments then carries the tag to
# github.com/streetfortress/s3-exporter, where .github/workflows/release.yml
# builds the image and packages the chart, both at ${TAG#v}. The pusher
# reconciles on an interval, so the release starts after that delay and not
# at once. Accepts "1.2.3" or "v1.2.3". Fetches first (pruning tags deleted
# on the remote) and refuses to tag unless HEAD is exactly <remote>/main, so
# a stale checkout can't ship a release.
tag version:
    #!/usr/bin/env bash
    set -euo pipefail
    remote=$(git remote -v | awk '/gitea\.zen\.lofi.*\(push\)/ {print $1; exit}')
    [[ -n "$remote" ]] || { echo "error: no gitea.zen.lofi remote configured" >&2; exit 1; }
    v="{{version}}"
    v="${v#v}"
    [[ "$v" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] \
        || { echo "error: 'v$v' is not vX.Y.Z semver" >&2; exit 1; }
    git fetch "$remote" --tags --prune --prune-tags
    [[ "$(git rev-parse HEAD)" == "$(git rev-parse "$remote/main")" ]] \
        || { echo "error: HEAD is not at $remote/main — pull first" >&2; exit 1; }
    git tag "v$v" && git push "$remote" "v$v"
