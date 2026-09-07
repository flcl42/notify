#!/bin/sh
# nfy's user-local installer. Review this script before running it if needed.
main() {
    set -eu
    case "$(uname -s)" in
        Linux) platform=linux ;;
        Darwin) platform=macos ;;
        *) echo 'Unsupported OS: use install.ps1 on Windows.' >&2; exit 1 ;;
    esac
    case "$(uname -m)" in
        x86_64|amd64) arch=x64 ;;
        aarch64|arm64) arch=arm64 ;;
        *) echo 'Unsupported architecture: x64 or arm64 required.' >&2; exit 1 ;;
    esac
    command -v curl >/dev/null || { echo 'curl is required.' >&2; exit 1; }
    if command -v sha256sum >/dev/null; then hash=sha256sum
    elif command -v shasum >/dev/null; then hash=shasum
    else echo 'sha256sum or shasum is required.' >&2; exit 1
    fi

    dir=${NFY_INSTALL_DIR:-"$HOME/.local/bin"}
    case "$dir" in /*) ;; *) echo 'NFY_INSTALL_DIR must be an absolute path.' >&2; exit 1 ;; esac
    tmp=$(mktemp -d)
    staged=
    trap 'rm -rf -- "$tmp"; if [ -n "$staged" ]; then rm -f -- "$staged"; fi' EXIT
    trap 'exit 1' HUP INT TERM
    asset="nfy-$platform-$arch"
    # Resolve latest once so a release published mid-install cannot mix assets.
    release=$(curl --proto '=https' --proto-redir '=https' -fsSL -o /dev/null -w '%{url_effective}' https://github.com/flcl42/notify/releases/latest)
    case "$release" in
        https://github.com/flcl42/notify/releases/tag/*) tag=${release#https://github.com/flcl42/notify/releases/tag/} ;;
        *) echo 'Unexpected release redirect.' >&2; exit 1 ;;
    esac
    case "$tag" in ''|*[!A-Za-z0-9._/-]*) echo 'Invalid release tag.' >&2; exit 1 ;; esac
    base="https://github.com/flcl42/notify/releases/download/$tag"
    curl --proto '=https' --proto-redir '=https' -fsSL "$base/$asset" -o "$tmp/$asset"
    curl --proto '=https' --proto-redir '=https' -fsSL "$base/SHA256SUMS.txt" -o "$tmp/SHA256SUMS.txt"
    expected=$(awk -v name="$asset" '$2 == name || $2 == "*" name {print $1}' "$tmp/SHA256SUMS.txt")
    case "$expected" in ''|*[!a-fA-F0-9]*) echo 'Missing or invalid release checksum.' >&2; exit 1 ;; esac
    [ "${#expected}" -eq 64 ] || { echo 'Invalid release checksum length.' >&2; exit 1; }
    if [ "$hash" = sha256sum ]; then actual=$(sha256sum "$tmp/$asset")
    else actual=$(shasum -a 256 "$tmp/$asset")
    fi
    actual=${actual%% *}
    [ "$actual" = "$expected" ] || { echo 'Checksum mismatch; existing installation unchanged.' >&2; exit 1; }

    mkdir -p "$dir"
    staged=$(mktemp "$dir/.nfy.XXXXXX")
    cp "$tmp/$asset" "$staged"
    chmod 755 "$staged"
    mv -f "$staged" "$dir/nfy"
    staged=
    # Only the executable is replaced; nfy.yaml and registered keys stay intact.
    if [ "$dir" = "$HOME/.local/bin" ]; then
        line='export PATH="$HOME/.local/bin:$PATH"'
        for rc in "$HOME/.profile" "$HOME/.bashrc" "$HOME/.zshrc"; do
            case "$rc" in
                */.bashrc) [ -f "$rc" ] || continue ;;
                */.zshrc) case "${SHELL:-}" in */zsh) ;; *) [ -f "$rc" ] || continue ;; esac ;;
            esac
            if ! grep -qxF "$line" "$rc" 2>/dev/null; then printf '\n%s\n' "$line" >> "$rc"; fi
        done
    fi
    "$dir/nfy" --version
    printf 'Installed: %s/nfy\n' "$dir"
    case ":$PATH:" in
        *":$dir:"*) ;;
        *) printf 'Open a new terminal, or add %s to PATH in this shell.\n' "$dir" ;;
    esac
}
main "$@"
