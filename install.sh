#!/usr/bin/env bash
set -euo pipefail

REPOSITORY="Aayush9029/OmaPad"
INSTALL_ROOT="${HOME}/.local"
SYSTEMD_ROOT="${HOME}/.config/systemd/user"
OMARCHY_ROOT="${HOME}/.config/omarchy/plugins/local.omapad"

fail() {
  printf 'OmaPad: %s\n' "$1" >&2
  exit 1
}

[[ "$(uname -s)" == "Linux" ]] || fail "Linux is required"
command -v curl >/dev/null || fail "curl is required"
command -v tar >/dev/null || fail "tar is required"
command -v systemctl >/dev/null || fail "systemd is required"
command -v bluetoothctl >/dev/null || fail "bluetoothctl is required. Install bluez-utils first"

case "$(uname -m)" in
  x86_64) architecture="amd64" ;;
  aarch64|arm64) architecture="arm64" ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

version="${OMAPAD_VERSION:-}"
if [[ -z "${version}" ]]; then
  latest_url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPOSITORY}/releases/latest")"
  version="${latest_url##*/}"
fi
[[ "${version}" == v* ]] || fail "could not find the latest release"

archive="omapad_${version#v}_linux_${architecture}.tar.gz"
temporary_root="$(mktemp -d)"
trap 'rm -rf "${temporary_root}"' EXIT

printf 'Installing OmaPad %s for linux/%s\n' "${version}" "${architecture}"
curl -fsSL "https://github.com/${REPOSITORY}/releases/download/${version}/${archive}" -o "${temporary_root}/${archive}"
tar -xzf "${temporary_root}/${archive}" -C "${temporary_root}"

install -Dm755 "${temporary_root}/omapad" "${INSTALL_ROOT}/bin/omapad"
install -Dm644 "${temporary_root}/packaging/systemd/omapad.service" "${SYSTEMD_ROOT}/omapad.service"

systemctl --user daemon-reload
systemctl --user enable --now omapad.service

if command -v omarchy >/dev/null && command -v omarchy-shell >/dev/null; then
  install -Dm644 "${temporary_root}/omarchy/local.omapad/manifest.json" "${OMARCHY_ROOT}/manifest.json"
  install -Dm644 "${temporary_root}/omarchy/local.omapad/Panel.qml" "${OMARCHY_ROOT}/Panel.qml"
  install -Dm644 "${temporary_root}/omarchy/local.omapad/Encouragements.js" "${OMARCHY_ROOT}/Encouragements.js"
  omarchy plugin validate "${OMARCHY_ROOT}"
  omarchy-shell -q shell rescanPlugins
  omarchy plugin enable local.omapad --before omarchy.bluetooth
  printf 'Omarchy widget installed\n'
fi

printf 'OmaPad is ready. Run: omapad\n'

