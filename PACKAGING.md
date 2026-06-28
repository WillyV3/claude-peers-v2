# Packaging / distribution

Releases are cut with [GoReleaser](https://goreleaser.com): `goreleaser release --clean`
(needs `GITHUB_TOKEN`). Tag a version first (`git tag vX.Y.Z && git push --tags`).

## Shipped automatically (every release)

| Channel | Install | Platforms |
|---|---|---|
| **Homebrew** | `brew install WillyV3/tap/claude-peers-v2` | macOS + Linux, amd64/arm64 |
| **Debian/Ubuntu** | `sudo dpkg -i claude-peers-v2_*_linux_amd64.deb` | amd64/arm64 |
| **Fedora/RHEL/openSUSE** | `sudo rpm -i claude-peers-v2_*_linux_amd64.rpm` | amd64/arm64 |
| **Alpine** | `sudo apk add --allow-untrusted claude-peers-v2_*_linux_amd64.apk` | amd64/arm64 |
| **raw binaries** | download the `.tar.gz` from Releases | macOS + Linux, amd64/arm64 |

`.deb/.rpm/.apk` are attached to each GitHub Release. All built pure-Go (no cgo).

## AUR (yay) — needs a one-time AUR account

Blocked autonomously: the release host's SSH key isn't registered with an AUR account.
One-time setup, then it publishes on every release:

1. Create an AUR account at https://aur.archlinux.org and add your SSH **public** key
   (`~/.ssh/id_ed25519.pub`) under your AUR profile.
2. Export the matching **private** key for GoReleaser: `export AUR_KEY="$(cat ~/.ssh/id_ed25519)"`.
3. Add this block to `.goreleaser.yaml`, then re-release:

```yaml
aurs:
  - name: claude-peers-v2-bin
    homepage: "https://github.com/WillyV3/claude-peers-v2"
    description: "Channel-native peer messaging network for AI coding agents"
    maintainers: ["WillyV3 <noreply@github.com>"]
    license: "MIT"
    private_key: "{{ .Env.AUR_KEY }}"
    git_url: "ssh://aur@aur.archlinux.org/claude-peers-v2-bin.git"
    package: |-
      install -Dm755 "./cpv2" "${pkgdir}/usr/bin/cpv2"
      install -Dm755 "./cpv2-tui" "${pkgdir}/usr/bin/cpv2-tui"
```

Then: `yay -S claude-peers-v2-bin`.

## Nix — needs nix installed + a NUR-style repo

Blocked autonomously: the release host has no `nix` binary (GoReleaser skips the nix pipe:
"nix-hash is not available"). One-time setup:

1. Install nix on the release machine (the `nix-hash` tool must be on PATH).
2. Create a repo `WillyV3/nur-packages` (a NUR-style flake/derivation repo).
3. Add this block to `.goreleaser.yaml`, then re-release:

```yaml
nix:
  - name: claude-peers-v2
    repository:
      owner: WillyV3
      name: nur-packages
    homepage: "https://github.com/WillyV3/claude-peers-v2"
    description: "Channel-native peer messaging network for AI coding agents"
    license: "mit"
    install: |
      mkdir -p $out/bin
      cp -vr ./cpv2 $out/bin/cpv2
      cp -vr ./cpv2-tui $out/bin/cpv2-tui
```

Then nix users install from the NUR/flake.
