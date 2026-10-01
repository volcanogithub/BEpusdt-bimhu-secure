#!/usr/bin/env python3
"""Read-only static publication isolation checks; never builds or publishes."""
import collections
import pathlib
import re
import subprocess
import yaml

REPO = "volcanogithub/BEpusdt-bimhu-secure"
BRANCH = "security/b2-hardening-v1.24.2"
FORBIDDEN = re.compile(r"DOCKER_HUB_BETA_TOKEN|DOCKERHUB_USERNAME|DOCKERHUB_TOKEN|DOCKER_HUB|v03413/bepusdt")
PATTERN = r"v03413/bepusdt|v03413|DOCKER_HUB|DOCKERHUB|docker/login-action|docker/build-push-action|push:[[:space:]]*true|ghcr.io|packages:[[:space:]]*write|GITHUB_TOKEN"
EXPECTED = ("github.repository == 'volcanogithub/BEpusdt-bimhu-secure' && "
            "github.ref == 'refs/heads/security/b2-hardening-v1.24.2' && "
            "(github.event_name == 'schedule' || github.event_name == 'workflow_dispatch')")

def git(*args):
    return subprocess.check_output(["git", *args], text=True)

def documents(ref):
    paths = [p for p in git("ls-tree", "-r", "--name-only", ref).splitlines()
             if p.startswith(".github/workflows/") and p.endswith((".yml", ".yaml"))]
    result = {}
    for path in paths:
        text = git("show", ref + ":" + path)
        result[path] = (yaml.load(text, Loader=yaml.BaseLoader), text)
    assert result, ref
    return result

def audit(ref, hardened):
    docs = documents(ref)
    for path, (doc, text) in docs.items():
        assert isinstance(doc, dict) and "on" in doc and "jobs" in doc, (ref, path)
        assert not FORBIDDEN.search(text), (ref, path, "legacy publication target/credentials")
        print("PASS workflow YAML/residue:", ref, path, "events", sorted(doc["on"]))
        for job in doc["jobs"].values():
            for step in job.get("steps", []):
                action = step.get("uses", "")
                inputs = step.get("with", {})
                if action.startswith("docker/login-action"):
                    assert hardened and path.endswith("/docker-nightly.yml"), (ref, path)
                    assert inputs.get("registry") == "${{ env.REGISTRY }}"
                    assert inputs.get("password") == "${{ secrets.GITHUB_TOKEN }}"
                if action.startswith("docker/build-push-action"):
                    if inputs.get("push") == "true":
                        assert hardened and path.endswith("/docker-nightly.yml"), (ref, path)
                        assert all(tag.startswith("${{ env.IMAGE }}:")
                                   for tag in inputs["tags"].splitlines() if tag)
                    else:
                        assert inputs.get("push") == "false", (ref, path)
                    assert "type=registry" not in inputs.get("outputs", ""), (ref, path)
        if not hardened:
            assert "docker/login-action" not in text, (ref, path)
            assert "packages: write" not in text, (ref, path)
    cfg = yaml.load(git("show", ref + ":.goreleaser.yaml"), Loader=yaml.BaseLoader)
    assert not any("docker" in key.lower() for key in cfg), (ref, "GoReleaser Docker publication")
    if hardened:
        doc, text = docs[".github/workflows/docker-nightly.yml"]
        assert doc["env"]["REGISTRY"] == "ghcr.io"
        assert doc["permissions"] == {"contents": "read"}
        assert doc["concurrency"] == {"group": "bimhu-ghcr-nightly", "cancel-in-progress": "false"}
        job = doc["jobs"]["build"]
        assert " ".join(job["if"].split()) == EXPECTED
        assert job["permissions"] == {"contents": "read", "packages": "write"}
        checkout = next(s for s in job["steps"] if s.get("uses", "").startswith("actions/checkout"))
        assert checkout["with"]["ref"] == "${{ github.sha }}"
        assert checkout["with"]["persist-credentials"] == "false"
        build = next(s for s in job["steps"] if s.get("uses", "").startswith("docker/build-push-action"))
        assert set(build["with"]["platforms"].split()) == {"linux/amd64", "linux/arm64"}
        assert "org.opencontainers.image.ref.name=" + BRANCH in build["with"]["labels"]
        assert "${{ env.IMAGE }}:sha-${{ env.SOURCE_REVISION }}" in build["with"]["tags"]
        assert 'IMAGE=ghcr.io/${IMAGE_OWNER}/bepusdt-bimhu-secure' in text
        # The asserted expression's context matrix; no live dispatch or publish.
        def permitted(repo, ref, event):
            return repo == REPO and ref == "refs/heads/" + BRANCH and event in ("schedule", "workflow_dispatch")
        assert permitted(REPO, "refs/heads/" + BRANCH, "workflow_dispatch")
        for ref in ["refs/heads/main", "refs/heads/unrelated", "refs/tags/v1.24.2"]:
            assert not permitted(REPO, ref, "workflow_dispatch")
        assert not permitted("other/fork", "refs/heads/" + BRANCH, "workflow_dispatch")
        assert not permitted(REPO, "refs/heads/" + BRANCH, "push")
        assert not permitted(REPO, "refs/heads/main", "schedule")
        print("PASS authorized-source/concurrency/SHA/multiarch assertions")

def residue(ref):
    proc = subprocess.run(["git", "grep", "-l", "-I", "-E", PATTERN, ref, "--"],
                          text=True, capture_output=True)
    assert proc.returncode in (0, 1), proc.stderr
    counts = collections.Counter()
    for item in proc.stdout.splitlines():
        path = item.split(":", 1)[1]
        if path.startswith(".github/workflows/"):
            category = "D" if ("GITHUB_TOKEN" in git("show", ref + ":" + path)
                               or "push: true" in git("show", ref + ":" + path)) else "C"
        elif path == ".goreleaser.yaml":
            category = "D (repository-local GitHub release; no Docker)"
        elif path.startswith("docs/") or path.startswith("README") or path.endswith(".go") or path == "go.mod":
            category = "A/B (reference/import compatibility or documented install dependency)"
        elif path.startswith("scripts/ci/"):
            category = "C (static audit; not publisher)"
        else:
            category = "B (runtime/build asset/dependency/reference)"
        counts[category] += 1
        print("RESIDUE", ref, category, path)
    print("RESIDUE COUNTS", ref, dict(counts))

audit("HEAD", True)
residue("HEAD")
# Check every current branch, and inventory tags. Old reachable commits are not rewritten.
branches = git("for-each-ref", "--format=%(refname)", "refs/remotes/origin/").splitlines()
for ref in branches:
    if ref.endswith("/HEAD") or ref.endswith("/" + BRANCH):
        continue
    audit(ref, False)
    residue(ref)
tags = git("tag", "--list").splitlines()
print("TAG INVENTORY", tags)
assert not tags, "Nonempty historical tags require explicit publication residue review"
assert git("status", "--porcelain") == "", "dirty cloud checkout"
print("PASS publication isolation static audit (no live publication performed)")
