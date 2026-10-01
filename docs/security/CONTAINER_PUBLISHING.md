# Container publication — GHCR nightly migration

Status: **PASS WITH DOCUMENTED LIMITATIONS** for current Phase B2-3 publication isolation. See the current isolation and validation sections below.

## Initial GHCR migration record (historical)

Initial status was PASS WITH LIMITATIONS: static verification only; publication had not yet been verified. The later historical #2 run and current isolation evidence supersede that initial assessment.

Registry: `ghcr.io`. Image: `ghcr.io/volcanogithub/bepusdt-bimhu-secure`.
The owner is supplied by `github.repository_owner` and converted to lowercase in Bash.

Nightly uses `docker/login-action@v3`, `github.actor`, and the automatic Actions `GITHUB_TOKEN`. No PAT or Docker Hub token is required. Workflow permission is `contents: read`; only the publishing job additionally has `packages: write`. Checkout does not persist credentials. Only schedule and manual dispatch may publish, with no PR publication trigger. No secret values are echoed.

Targets: `linux/amd64`, `linux/arm64`. Tags: `nightly` and `nightly-YYYYMMDD-<7-character-source-commit>`. UTC dates follow the runner clock. OCI labels identify the source repository and actual checked-out commit. The existing lowercase `dockerfile` is selected explicitly. The version base falls back to `v1.24.2` when no Git release tags exist in this imported repository.

The schedule remains `0 16 * * *` (00:00 Asia/Shanghai). Scheduled builds check out `security/b2-hardening-v1.24.2`; manual builds use the selected branch. The inherited `docker-latest` publisher is disabled, its Docker Hub authentication and upstream image destination removed, and push set to false. A release/version container publisher is not introduced.

## Baseline failure evidence

Run `36772820558`, source `c0b66ef91332eb68caeb501f670fc83ee11940ae`, failed at `Login to DockerHub`. The downloaded job log reports `Password required`. This confirms an empty password input at login; it does not prove whether the original secret was absent or unavailable. Version generation and build/push were skipped. The inherited workflow referenced another maintainer's Docker Hub account and secret name.

## Repository-wide residue audit

- Historical/reference: upstream links, badges, Go module/import paths, authorship and baseline provenance, UI documentation links, API integration examples. Retained for attribution and compatibility.
- Runtime dependencies: Dockerfile pulls `node:25.2.1`, `golang:1.26.2-alpine3.23`, `alpine:3.20`; Go depends on upstream `go-cache` and `tronprotocol`. Retained; these are dependency pulls, not upstream publication.
- Runtime installation examples: upstream image references remain in `README.md`, `docs/docker/docker.md`, `docs/1panel/README.md`, and `docs/bt_panel/README.md`. These describe upstream images and do not contain this fork's hardening. They are not a deployment recommendation for this fork; no deployment docs are rewritten in this migration.
- Publication/authentication: nightly migrated; `docker-latest` blocked and credentials removed. `pr-check.yml` has `push: false`. `release.yml` uses repository-scoped `GITHUB_TOKEN` for GitHub releases; `.goreleaser.yaml` has no upstream Docker publication destination. Retained without introducing a release pipeline.

Audit command: `git grep -n -I -E 'v03413|bepusdt|docker.io|hub.docker.com|DOCKER_HUB|DOCKERHUB|docker/login-action|docker/build-push-action|ghcr.io|secrets\.|registry|push:|packages:'`. All tracked files are inspected; lockfile/import matches are classified as dependencies.

## Verification and manual run

YAML parsing and static assertions cover triggers, permissions, GHCR login, lowercase owner, tags, architectures, Dockerfile selection, and removal of upstream workflow credentials/destinations. `git diff --check` is required.

Docker build NOT RUN: local Docker daemon is unavailable. Actions dispatch NOT RUN: local GitHub CLI authentication is invalid and the connected plugin offers no dispatch endpoint. New image/tags/digest/architectures/package visibility are NOT VERIFIED. No package visibility change is performed.

Open the repository's Actions page, select `docker-nightly`, choose **Run workflow**, select **security/b2-hardening-v1.24.2**, then run. Observe Checkout, QEMU, Buildx, Login to GHCR, Generate App Version, and Build and push. Verify the GHCR package tags, digest, and both architecture manifests after success.

Important: changes exist on the security branch only. The default `main` branch is preserved as the original baseline, including its inherited publishers. GitHub scheduled runs use the default-branch workflow, so this branch-only change does not activate the GHCR schedule or disable the old main workflow. A separate reviewed default-branch workflow update or repository-level disable is required; it is not performed here. Do not run the inherited main publisher.

References: [GitHub GHCR publication](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images), [workflow events/default branch requirements](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows).

No payment/business code, database schema, API behavior, credential hardening, wallet logic, production configuration, or BIMHU CREDIT integration is changed.

## Phase B2-3 publication isolation (2026-10-01)

This section supersedes the initial migration's obsolete static-only/default-main
warnings above; they are retained as historical evidence, not current guidance.
Status: **PASS WITH DOCUMENTED LIMITATIONS**. No production readiness claim.

### Safety snapshot before changes

Remote repository: `https://github.com/volcanogithub/BEpusdt-bimhu-secure.git`;
SSH URL `git@github.com:volcanogithub/BEpusdt-bimhu-secure.git`.
Default branch: `main`, HEAD `ee1e24d1440ce82065dc247306481ab98bf6db60`.
Security branch HEAD: `058b04d692f530209d417520d0896be1f95cf0f8`.
Changes use GitHub Git objects and cloud Actions, not a local checkout.
Local `git status`/`git remote -v`: **NOT RUN — no local worktree**.
Cloud checkout clean-tree/diff assertions provide the actual worktree evidence;
remote ref/clone URLs are API-verified, not fabricated shell output.

### Preserve previous GHCR verification

[Run 36801425308, docker-nightly #2](https://github.com/volcanogithub/BEpusdt-bimhu-secure/actions/runs/36801425308)
was manually triggered on `security/b2-hardening-v1.24.2`, source commit
`058b04d692f530209d417520d0896be1f95cf0f8`, after the original migration
`85f7c759cf84e26c2ab10c1cce9ab3b846593d61`. It is **SUCCESS**.
Job `110176401481`: checkout, QEMU, Buildx, GHCR login, version generation and
build/push all succeeded, as did post steps. It was already complete and was not cancelled,
rerun or modified to falsify evidence.

Image: `ghcr.io/volcanogithub/bepusdt-bimhu-secure`.
Published tags logged: `nightly`, `nightly-20261001-058b04d`.
Manifest-list digest logged:
`sha256:63a8d9d5b45c6715b622f49092f19f674d97957d1562933151ff96de5afce92e`.
The log confirms successful amd64/arm64 build stages, two platform manifests and
a pushed manifest list. Independent live registry manifest/platform inspection:
**NOT VERIFIED**. Package visibility: **NOT VERIFIED**.
Current mutable-tag contents need not still equal this historical digest.

### Default-branch architecture and legacy removal

Main is baseline/reference only. [PR #1](https://github.com/volcanogithub/BEpusdt-bimhu-secure/pull/1)
previously merged as `ee1e24d1440ce82065dc247306481ab98bf6db60`.
Its nightly/latest are manual, build-only: no schedule/create publisher, no registry
login, no inherited credentials, `push:false`, `contents:read`; no `packages:write`.
Only those two workflow files changed; application and reference docs were preserved.
YAML/grep/diff/original Go tests/build passed in run 36804441955; existing PR
Docker amd64 build passed in run 36805636424. This phase does not automatically
merge any further main changes or migrate main to hardened GHCR tags.

All main workflows and publication configuration, not just nightly, are audited.
`release.yml` still uses repository-scoped GITHUB_TOKEN/GoReleaser for **this
repository's GitHub releases**, not upstream containers; no Docker publisher is
configured in `.goreleaser.yaml`. Broad create triggering remains a limitation.
Prior incidental run 36804442151 failed because there were no Git tags; no release
was created. It must not be treated as an isolation mechanism.

GitHub schedule definitions run from the default branch. Main has no GHCR/Docker
publishing schedule, so the security branch's retained cron is currently **inert**.
This phase does NOT activate a scheduler or copy hardened publishing to main.
If a scheduler is later needed, separately design a tested authorized-source
dispatch/control path; merely copying the security workflow to main would fail its
strict source-ref gate. No arbitrary main/manual source may update hardened tags.

### Hardened publisher controls

Registry/image remain BIMHU-controlled GHCR. Authentication uses only automatic
`GITHUB_TOKEN` via `github.actor`; only the build job has `packages:write`.
No PAT or Docker Hub credentials. Job condition requires the exact repository,
`refs/heads/security/b2-hardening-v1.24.2`, and dispatch/schedule event.
Main refs, other branches, tags, forks and push events cannot enter this job.
Checkout pins `github.sha` rather than resolving a moving branch name.

Fixed workflow concurrency group `bimhu-ghcr-nightly`, cancel-in-progress:false,
permits only one active run in this group; no different-ref groups can race for
the shared nightly tag. GitHub may replace an older pending run with a newer one;
there is no FIFO guarantee. Manual controls in historical workflow revisions are
not retroactively changed; job cancellation cannot roll back a finished push.

Architectures remain linux/amd64 and linux/arm64. Tags: nightly,
nightly-YYYYMMDD-<7-char SHA>, and sha-<full source SHA>. OCI labels record repository,
exact SHA and authorized source branch. Tags are mutable registry names, including
SHA-named tags on rebuild; **digest pinning** supplies content identity. This is not
a claim that a tag alone is immutable or that a rebuild is reproducible.

The publisher has no `needs` link to full regression/security acceptance. Existing
tests are separately exercised by Confirmation Policy CI; building an image is
not security validation. Adding a meaningful publication gate is recorded for a
later phase, not silently implemented as a CI redesign.

### Residue classification and secrets

A — historical/reference: README badges/upstream authorship, audit history and
Go module/import paths (compatibility/provenance). Preserve.
B — runtime/install dependency: Dockerfile base-image pulls, upstream go-cache/
tronprotocol dependencies; installation examples in README.md, docs/docker/docker.md,
docs/1panel/README.md, docs/bt_panel/README.md. Their upstream images **do not contain
BIMHU hardening** and are not deployment guidance for this fork.
C — build-only CI: main nightly/latest and pr-check; push:false, no registry auth.
D — publication/auth: authorized security GHCR and repository-local GitHub release.
Upstream container publication category D must be zero in the audited current refs.

Obsolete names: DOCKER_HUB_BETA_TOKEN, DOCKERHUB_USERNAME, DOCKERHUB_TOKEN.
Actual repository secret presence: **NOT VERIFIED**; no secret API access,
deletion, credential request, PAT creation or secret value output.
Historical commits/docs remain immutable evidence, not current executable configuration.
Older non-maintained branches/tags with inherited workflows can require separate
inventory/cleanup authorization; this audit inventories refs and reports any found
legacy executable revision rather than claiming old history was erased.

Validation commands and final CI links are recorded below after execution.
No business/payment/blockchain/schema/API behavior changes, deployment, wallet
access, real payment or CREDIT. Signals remain UNTRUSTED_HINT.

### Executed isolation acceptance

Functional commit: `7d13aeb286d94c3af04891f14322442dad6f6089`.
[Cloud run 36807055783](https://github.com/volcanogithub/BEpusdt-bimhu-secure/actions/runs/36807055783),
job `110193641336`: **PASS**. Go 1.26.2 / isolated PostgreSQL 17.11.
Before checkout changes, remote branch/ref snapshots were recorded; the cloud checkout
was clean, and the final git status/diff gate passed. Remote origin was
`https://github.com/volcanogithub/BEpusdt-bimhu-secure` for fetch and push.

Commands actually run:
```bash
python -m pip install 'PyYAML==6.0.2'
python scripts/ci/publication_isolation.py
git diff --check 058b04d692f530209d417520d0896be1f95cf0f8 HEAD
git diff --exit-code 058b04d692f530209d417520d0896be1f95cf0f8 HEAD -- app main go.mod go.sum dockerfile web static README.md docs/docker docs/1panel docs/bt_panel .goreleaser.yaml
git remote -v
git status --porcelain
go test -p 1 -count=20 -run '^TestB1ConcurrentWorkersSendOnceAtATime$' -v ./app/task/notify
go test -p 1 -count=20 -run '^TestB1R2SQLite' -v ./app/model
go test -p 1 -count=1 -run 'TestB1R.*Postgres' -v ./app/model ./app/task/notify
go test -p 1 -count=1 -run TestB23 -v ./app/model ./app/task
go test -p 1 -count=1 ./...
GOFLAGS=-o=/tmp/bepusdt-b2-3 go build ./main
git diff --check
test -z "$(git status --porcelain)"
```

The audit script uses git ls-tree/show/grep/for-each-ref/tag to inspect all
workflow definitions, each current remote branch and all matching tracked files.
Current refs: main, security/b2-hardening-v1.24.2,
audit/main-publication-isolation-validation. **Tag inventory: empty**.
All workflow YAML parsed successfully. Current upstream executable category D
matches: **zero**. Obsolete secret references in workflow definitions: **zero**.
Repository-wide reference/dependency matches remain intentionally preserved.

| Invariant | Result | Evidence |
| --- | --- | --- |
| INV-B2-3-01 | PASS | Current branch workflow targets and GoReleaser configuration contain no upstream container publisher |
| INV-B2-3-02 | PASS | Forbidden inherited credential-reference scan zero across current workflows |
| INV-B2-3-03 | PASS | Main only build-only manual Docker workflows; other schedules are issue maintenance, not container publication |
| INV-B2-3-04 | PASS | Exact repository gate plus lowercased owner-derived BIMHU GHCR image |
| INV-B2-3-05 | PASS | Exact authorized ref/event expression, pinned checkout SHA, context rejection matrix |
| INV-B2-3-06 | PASS | GITHUB_TOKEN only; no credential creation/deletion or PAT addition |
| INV-B2-3-07 | PASS | Original application/blockchain/schema/API source diff empty |
| INV-B2-3-08 | PASS | PyYAML all workflows plus policy assertions and git diff checks |
| INV-B2-3-09 | PASS | Historical/import/install references retained with explicit non-hardened-image notice |
| INV-B2-3-10 | PASS | Original run 36801425308 / source / tags / digest recorded without altering its result |

Original worker race **20/20 PASS**, six SQLite cases **120/120 PASS**;
PostgreSQL/fault recovery/B2-3 and full Go regression/build **PASS**.
Current static isolation controls are tested, but this is NOT a live rerun of
the modified publisher. Docker image rebuild and live GHCR publication in this phase:
**NOT RUN**; no claim of Docker-daemon unavailability is made (the cloud test runner
has a daemon). Earlier historical GHCR multiarch build and main PR amd64 build
are separately identified, not relabelled as current publisher acceptance.
Package visibility and independent registry-manifest inspection remain **NOT VERIFIED**.

Main is unchanged in this phase at ee1e24d1440ce82065dc247306481ab98bf6db60.
No new main PR or automatic merge was performed. Security commits are fast-forwards;
history is not rewritten. This completion is scoped to audited current refs.
Archived workflow revisions/history and privileged future edits are not made safe
retroactively; old workflow reruns require review rather than blind reuse.
