#!/bin/sh
set -eu
installer=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)/install.sh
test_root=$(mktemp -d)
trap 'rm -rf -- "$test_root"' EXIT
mkdir -p "$test_root/bin" "$test_root/home" "$test_root/assets" "$test_root/work"
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
printf 'fake-apk-bytes\n' > "$TEST_ASSETS/private-notify-android.apk"
(cd "$TEST_ASSETS" && sha256sum private-notify-android.apk) >> "$TEST_ASSETS/SHA256SUMS.txt"
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
# No APK without the flag, even though the release contains one.
cd "$test_root/work"
rm -f ./private-notify-android.apk
TEST_OS=Linux TEST_ARCH=x86_64 sh "$installer"
test ! -e ./private-notify-android.apk
# Unknown options are rejected.
if sh "$installer" --bogus; then echo 'FAIL: unknown option accepted' >&2; exit 1; fi
# --apk downloads the checksum-verified APK into the current directory.
TEST_OS=Linux TEST_ARCH=x86_64 sh "$installer" --apk
cmp "$TEST_ASSETS/private-notify-android.apk" ./private-notify-android.apk
cmp "$TEST_ASSETS/binary" "$HOME/.local/bin/nfy"
# Corrupt APK is rejected; CLI and existing APK stay intact.
printf 'corrupt apk\n' > "$TEST_ASSETS/private-notify-android.apk"
if TEST_OS=Linux TEST_ARCH=x86_64 sh "$installer" --apk; then echo 'FAIL: corrupt APK accepted' >&2; exit 1; fi
cmp "$TEST_ASSETS/binary" "$HOME/.local/bin/nfy"
printf 'fake-apk-bytes\n' > "$TEST_ASSETS/private-notify-android.apk"
cd - >/dev/null
printf 'corrupt binary\n' > "$TEST_ASSETS/nfy-linux-x64"
if sh "$installer"; then echo 'FAIL: checksum mismatch accepted' >&2; exit 1; fi
cmp "$TEST_ASSETS/binary" "$HOME/.local/bin/nfy"
if TEST_ARCH=riscv64 sh "$installer"; then echo 'FAIL: unknown arch accepted' >&2; exit 1; fi
echo 'PASS: platforms, architectures, checksums, atomic update, config preservation, PATH deduplication, opt-in APK'
