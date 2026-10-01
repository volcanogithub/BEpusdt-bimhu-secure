# Container publication — GHCR nightly migration

Status: PASS WITH LIMITATIONS. Static verification only; image publication NOT VERIFIED.

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
