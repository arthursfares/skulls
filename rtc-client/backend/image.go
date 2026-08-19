package backend

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------
// image sharing: /image fetches a local file or an http(s) URL entirely on
// this client, downscales and re-encodes it, and broadcasts the result over
// every peer's chat data channel so it can be rendered inline in the chat
// log via the kitty graphics protocol.
// ----------------------------------------------------------------------

const (
	maxImageSourceBytes = 20 << 20 // sanity cap on the source file/response size (20 MiB)
	maxImageDim         = 1024     // longest edge, in pixels, images are downscaled to before broadcast
	imageJPEGQuality    = 82
)

// NOTE. LoadImageCmd resolves source (a local file path or an http(s) URL), decodes
// and downscales it, broadcasts the result (with caption, if any) to every
// peer, and hands it back to the UI (as an ImageMsg with From == "") so the
// sender also sees it in their own chat log.
func LoadImageCmd(source, caption string) tea.Cmd {
	return func() tea.Msg {
		raw, err := readImageSource(source)
		if err != nil { return ErrorMsg{Reason: "couldn't load image: " + err.Error()} }
		img, _, err := image.Decode(bytes.NewReader(raw))
		if err != nil { return ErrorMsg{Reason: "couldn't decode image: " + err.Error()} }
		img = downscaleImage(img, maxImageDim)

		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: imageJPEGQuality}); err != nil { return ErrorMsg{Reason: "couldn't encode image: " + err.Error()} }
		BroadcastImageToAllPeers(buf.Bytes(), caption)
		return ImageMsg{Image: img, Caption: caption}
	}
}

func readImageSource(source string) ([]byte, error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Get(source)
		if err != nil { return nil, err }
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK { return nil, fmt.Errorf("http %d", resp.StatusCode) }
		return io.ReadAll(io.LimitReader(resp.Body, maxImageSourceBytes))
	}
	f, err := os.Open(source)
	if err != nil { return nil, err }
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxImageSourceBytes))
}

// ----------------------------------------------------------------------

// NOTE. downscaleImage nearest-neighbor-scales img down so its longer edge is at
// most maxDim pixels, preserving aspect ratio. Images already within budget
// are returned unchanged. This keeps the data-channel payload (and the
// terminal-side transmit) small without pulling in an image-scaling
// dependency for what only needs to look reasonable at a few dozen terminal
// cells wide.
func downscaleImage(img image.Image, maxDim int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxDim && h <= maxDim { return img }
	var newW, newH int
	if w >= h {
		newW = maxDim
		newH = h * maxDim / w
	} else {
		newH = maxDim
		newW = w * maxDim / h
	}
	if newW < 1 { newW = 1 }
	if newH < 1 { newH = 1 }
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		sy := b.Min.Y + y*h/newH
		for x := 0; x < newW; x++ {
			sx := b.Min.X + x*w/newW
			dst.Set(x, y, img.At(sx, sy))
		}
	}
	return dst
}
