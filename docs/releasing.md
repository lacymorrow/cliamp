# Releasing

Releases are fully automated via GitHub Actions and [GoReleaser](https://goreleaser.com/). Pushing a version tag triggers the pipeline.

## How to Release

```sh
git tag v1.2.3
git push origin v1.2.3
```

That's it. The pipeline handles everything else.

## What Happens

### 1. Matrix Build (`build` job)

Four parallel jobs compile the binary natively — one per target:

| Target | Runner |
|--------|--------|
| linux/amd64 | ubuntu-latest (native) |
| linux/arm64 | ubuntu-latest (cross-compile via `aarch64-linux-gnu-gcc`) |
| darwin/amd64 | macos-13 (Intel runner, native) |
| darwin/arm64 | macos-latest (Apple Silicon, native) |

CGO is required for audio output (ALSA on Linux, CoreAudio on macOS), so each platform compiles on its own runner rather than cross-compiling through a single host. The macOS amd64 target uses the `macos-13` runner (a real Intel machine) instead of Rosetta 2.

Each job uploads its `dist/` output as a GitHub Actions artifact.

### 2. GitHub Release (`release` job)

GoReleaser downloads all four `dist/` artifacts, assembles them into archives, and publishes the GitHub Release:

- `cliamp-linux-amd64.tar.gz`
- `cliamp-linux-arm64.tar.gz`
- `cliamp-darwin-amd64.tar.gz`
- `cliamp-darwin-arm64.tar.gz`
- `checksums.txt` (SHA256)

Each archive contains the binary, `LICENSE`, `README.md`, and `config.toml.example`.

If `AUR_KEY` is set in repository secrets, GoReleaser also publishes a `cliamp-bin` pre-built binary package to AUR. If not set, this step is skipped and the release continues normally.

### 3. Homebrew Tap (`update-homebrew` job)

After the release is published, a second job generates a Homebrew formula and pushes it to [bjarneo/homebrew-cliamp](https://github.com/bjarneo/homebrew-cliamp). It reads the SHA256 checksums directly from the published `checksums.txt` and writes `Formula/cliamp.rb` with the correct URLs and hashes for each platform/arch combination.

Homebrew automatically extracts the `.tar.gz` archive before installation — the formula's `install` block just moves the extracted binary into place.

Uses `GITHUB_TOKEN` (the automatic Actions token, or a configured PAT if cross-repo write access is needed).

### 4. AUR Source Package (`aur.yml`)

A separate workflow (`aur.yml`) triggers on Release completion and publishes the source-based `cliamp` package to AUR. It downloads the source tarball for the tag, computes its SHA256, generates a `PKGBUILD` that builds from source, and pushes to AUR using `AUR_SSH_PRIVATE_KEY`.

This is independent of GoReleaser and handles the `cliamp` AUR package (as opposed to `cliamp-bin`).

## Secrets

| Secret | Required | Used for |
|--------|----------|----------|
| `GITHUB_TOKEN` | Yes (automatic) | GitHub Release, Homebrew tap push |
| `AUR_SSH_PRIVATE_KEY` | Yes | AUR source package (`aur.yml`) |
| `AUR_USERNAME` | Yes | AUR commit identity |
| `AUR_EMAIL` | Yes | AUR commit identity |
| `AUR_KEY` | No | GoReleaser `cliamp-bin` AUR package — skipped if absent |

## Configuration Files

- `.goreleaser.yml` — archive format, checksums, AUR bin package
- `.github/workflows/release.yml` — matrix build + GoReleaser release + Homebrew update
- `.github/workflows/aur.yml` — source-based AUR package

## Pre-releases

Tag with a pre-release suffix (e.g. `v1.2.3-beta.1`) and GoReleaser will mark the GitHub Release as a pre-release. The `skip_upload: auto` setting on the AUR bin package causes GoReleaser to skip the AUR push for pre-releases. The Homebrew tap is still updated.

## Local Build

To build a single binary locally:

```sh
go build -ldflags="-s -w" -o cliamp .
```

To test GoReleaser locally (build only, no publish):

```sh
goreleaser build --single-target --snapshot --clean
```
