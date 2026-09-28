#!/usr/bin/env bash
set -euo pipefail

REPOSITORY="Aayush9029/OmaPad"
INSTALL_ROOT="${HOME}/.local"
SYSTEMD_ROOT="${HOME}/.config/systemd/user"
PLUGIN_ID="io.github.aayush9029.omapad"
PLUGINS="${HOME}/.config/omarchy/plugins"

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
systemctl --user enable omapad.service
systemctl --user restart omapad.service

if command -v omarchy >/dev/null && command -v omarchy-shell >/dev/null; then
  # Earlier versions installed a local copy of the widget under another ID.
  if [[ -d "${PLUGINS}/local.omapad" ]]; then
    omarchy plugin remove local.omapad --yes >/dev/null 2>&1 || rm -rf "${PLUGINS}/local.omapad"
  fi
  if [[ -d "${PLUGINS}/${PLUGIN_ID}" ]]; then
    omarchy plugin update "${PLUGIN_ID}" --yes
  else
    omarchy plugin add "https://github.com/${REPOSITORY}" --yes
  fi
  omarchy plugin enable "${PLUGIN_ID}" --before omarchy.bluetooth
  printf 'Omarchy widget installed\n'
fi

printf 'OmaPad is ready. Run: omapad\n'

