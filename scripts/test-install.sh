#!/bin/sh
set -eu
installer=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)/install.sh
test_root=$(mktemp -d)
trap 'rm -rf -- "$test_root"' EXIT
mkdir -p "$test_root/bin" "$test_root/home" "$test_root/assets"
export HOME="$test_root/home" TEST_ASSETS="$test_root/assets" SHELL=/bin/sh
export PATH="$test_root/bin:$PATH"
cat > "$test_root/bin/uname" <<'MOCK'
#!/bin/sh
case "$1" in -s) echo "${TEST_OS:-Linux}" ;; -m) echo "${TEST_ARCH:-x86_64}" ;; esac
MOCK
cat > "$test_root/bin/curl" <<'MOCK'
#!/bin/sh
set -eu
output=
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o) output=$2; shift 2 ;;
        -w|--proto|--proto-redir) shift 2 ;;
        -fsSL) shift ;;
        *) url=$1; shift ;;
    esac
done
case "$url" in
    */releases/latest) printf '%s' https://github.com/flcl42/notify/releases/tag/release/test ;;
    */download/release/test/*) cp "$TEST_ASSETS/${url##*/}" "$output" ;;
    *) exit 1 ;;
esac
MOCK
chmod +x "$test_root/bin/curl" "$test_root/bin/uname"
printf '#!/bin/sh\necho "nfy version test"\n' > "$TEST_ASSETS/binary"
for asset in nfy-linux-x64 nfy-linux-arm64 nfy-macos-x64 nfy-macos-arm64; do
    cp "$TEST_ASSETS/binary" "$TEST_ASSETS/$asset"
    (cd "$TEST_ASSETS" && sha256sum "$asset") >> "$TEST_ASSETS/SHA256SUMS.txt"
done
mkdir -p "$HOME/.local/bin"
printf 'private-key-sentinel\n' > "$HOME/.local/bin/nfy.yaml"
for os in Linux Darwin; do
    for arch in x86_64 arm64; do
        TEST_OS=$os TEST_ARCH=$arch sh "$installer"
        cmp "$TEST_ASSETS/binary" "$HOME/.local/bin/nfy"
        test "$(cat "$HOME/.local/bin/nfy.yaml")" = private-key-sentinel
    done
done
test "$(grep -c 'export PATH=' "$HOME/.profile")" -eq 1
printf 'corrupt binary\n' > "$TEST_ASSETS/nfy-linux-x64"
if sh "$installer"; then echo 'FAIL: checksum mismatch accepted' >&2; exit 1; fi
cmp "$TEST_ASSETS/binary" "$HOME/.local/bin/nfy"
if TEST_ARCH=riscv64 sh "$installer"; then echo 'FAIL: unknown arch accepted' >&2; exit 1; fi
echo 'PASS: platforms, architectures, checksums, atomic update, config preservation, PATH deduplication'
