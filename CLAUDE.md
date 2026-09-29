# bo-catalog

Source for the `catalog` service. **Source only** — no manifests live here.

Canonical spec: [`bo-platform/BUILD-PLAN.md`](https://github.com/bo-jr/bo-platform/blob/main/BUILD-PLAN.md)
§4 (layout), Phase 2 (the service), Phase 6 scenario 5 (schema migration).
Where it and [`bo-platform/DECISIONS.md`](https://github.com/bo-jr/bo-platform/blob/main/DECISIONS.md)
disagree, `DECISIONS.md` wins.

## What this service is

`GET /items/{sku}`, backed by **CloudNativePG**. Seeded with ~50 rows.

It is the only service with a database, which makes it the subject of the hardest
scenario in the lab: **Phase 6 scenario 5, the expand-contract schema migration**, where
`catalog` v2 needs a new column and must promote together with `storefront` in a single
PR carrying both digests. That atomicity is the entire reason `rendered/` lives in one
shared `bo-deploy` repo rather than one per service.

Postgres credentials come from the `catalog-db-app` Secret that CloudNativePG generates,
referenced by name — never a literal Secret in git. Phase 7 moves its source to
**OpenBao via ESO** without changing the reference (DECISIONS 2026-09-29).

**The `Cluster` CR does not live here.** It is stateful infrastructure and lives in
`bo-platform/databases/<env>/catalog.yaml`, outside the release path — a bad release or a
`git revert` must never be able to delete the database. **The schema and the seed rows do
live here**, as SQL migrations embedded in the binary and applied at startup. That is what
lets a schema change promote atomically with the image digest.

## The name

**The `bo-` prefix stops at the repo boundary.** Inside the cluster this service is
`catalog` — Rollout, Service, `app` label, Prometheus `service=` label, SLO name.
Never `bo-catalog`. The image is the sole exception:
`ghcr.io/bo-jr/bo-catalog`, because that is what `ghcr.io/${{ github.repository }}`
resolves to. The chart takes `image.repository` **with** the prefix, `name` **without**.

## Required of every service, without exception

- `GET /healthz` (liveness), `GET /readyz` — for this service `readyz` must actually
  check the database connection, not return static 200
- `GET /metrics` — `http_requests_total{service,route,status,version}` and
  `http_request_duration_seconds` histogram, same labels
- OTel tracing, W3C traceparent propagation, OTLP export to the local Alloy
- Structured JSON logs to stdout including `trace_id`
- Graceful shutdown on SIGTERM with connection draining

Shared behaviour comes from `bo-service-kit`. Telemetry and chaos code belongs there.

## Chaos knobs

| Var | Effect |
|---|---|
| `FAILURE_RATE` | float 0.0–1.0; that fraction of requests return 500 |
| `EXTRA_LATENCY_MS` | int; sleep injected before responding |
| `APP_VERSION` | string; must appear as a Prometheus label and a pod label |

## What lives here

```
cmd/catalog/main.go
internal/migrate/             # embedded SQL migrations, applied at startup under an advisory lock
Dockerfile                    # multi-arch, built natively per arch
chart-values.yaml             # values for the shared chart
.github/workflows/ci.yml      # calls the reusable workflow
```

Pin `bo-service-chart` **by exact version** — the `chartVersion:` field of `chart-values.yaml`.

## What must never live here

- Rendered manifests — CI output, committed to `bo-deploy`
- A `manifests/` or `base/` directory — the shared chart replaces it
- Migration logic that assumes a stop-the-world deploy. Expand-contract only:
  add the column, backfill, read both, then drop. Two versions run at once during a
  canary — that is the point of the scenario.

## Metrics discipline

**Never label a Prometheus metric with a commit SHA, image digest, or Rollout hash.**
Unbounded cardinality. Those belong in GitHub Deployments and Discord messages.
## Non-negotiable (inherited from `bo-platform/CLAUDE.md`)

- **No floating tags. Ever.** Not `latest`, `lts`, `stable`, or partial semver (`:1`,
  `:1.2`). Images pinned by **manifest-list digest**, charts by exact semver.
- **Pin the index digest, never a per-arch digest.** GitHub Actions runners are
  `linux/amd64`; every cluster in the lab is `arm64`. A platform-specific digest pulls
  fine where you tested it and fails `no match for platform` on the other side of that
  boundary. This is the most likely portability bug in the lab.
- **Images build `linux/amd64,linux/arm64`.** The amd64 leg is what CI tests against;
  the arm64 leg is what actually deploys. The lab itself runs only on `darwin/arm64`.
- **LF line endings**, enforced by `.gitattributes`. A CRLF `.sh` inside a Linux image
  fails as `bad interpreter: /bin/bash^M`.
- **When something fails, check architecture first** — the usual cause of
  `ImagePullBackOff` and `exec format error` here.
- If reality contradicts the plan, **stop and say so.** Do not improvise around it;
  record the outcome in `bo-platform/DECISIONS.md`.
