# rtc-client

Terminal client for skulls — a TUI for voice calls and text chat in rooms hosted by a `signaling-server`. Built with Bubble Tea, and
uses WebRTC (pion) for peer-to-peer audio and Opus for encoding.

## Layout

- `main.go` — entrypoint, loads `.env`, starts the Bubble Tea program
- `screens/` — TUI screens and state machine (auth, dashboard, call), key
  bindings, command palette, and in-chat slash commands (`/help`,
  `/private`, `/roll`, `/play`, `/stop`)
- `components/` — reusable Bubble Tea components (lists, audio device
  picker) and shared view types
- `backend/` — networking and audio: signaling client (`client.go`),
  account/REST client (`accounts_client.go`), audio capture/playback
  (`audio.go`), media playback for `/play` (`media_playback.go`),
  per-peer volume control (`peer_volumes.go`), synthetic/bot peer support
  (`synthetic_peer.go`), local encrypted chat history (`history.go`)
- `styles/` — Lip Gloss styles shared across screens
- `install.sh` — installs the built binary plus runtime deps (libopus,
  ffmpeg, yt-dlp) on the target machine
- `package.sh` — builds the binary and bundles it with `install.sh` into a
  release tarball

## Running locally

Requires a running `signaling-server` to connect to.

```
export SIGNALING_SERVER_URL="http://localhost:8080"
go run .
```

Alternatively, set `SIGNALING_SERVER_URL` in a `.env` file in this
directory — it's loaded automatically on startup.

## Runtime dependencies

Audio capture/playback needs `libopus`, `libopusfile`, and `libogg`
installed. The `/play` chat command additionally needs `ffmpeg` and
`yt-dlp` on `PATH`. `install.sh` installs all of these for you when
setting up a packaged release.

`yt-dlp` breaks whenever YouTube changes something server-side, and
distro-packaged builds tend to lag behind the fix. `install.sh` installs
the standalone binary release instead (as `yt-dlp` upstream recommends)
and self-updates it (`yt-dlp -U`) each time it's re-run, so re-running
`install.sh` is the way to fix a broken `/play`.

## Building a release

```
./package.sh
```

Produces `dist/skulls-rtc-client-linux-amd64.tar.gz` containing the binary
and `install.sh`. Recipients extract it and run `./install.sh`, which
installs runtime dependencies, copies the binary to
`$RTC_CLIENT_INSTALL_DIR` (default `~/.local/bin`), fetches `yt-dlp`, and
prompts for a `SIGNALING_SERVER_URL` if one isn't already set.

> **If you already have a remote signaling server running**, edit
> `DEFAULT_SIGNALING_SERVER_URL` near the top of `install.sh` to point at
> it *before* running `package.sh`. Recipients then get that URL
> auto-configured on install, instead of a placeholder they have to
> track down and edit into their shell profile by hand.
