package jobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"maps"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	maxSourceBytes  = 20 << 20
	maxSourcePixels = 40_000_000
	maxSizes        = 8
	maxWidth        = 4096
)

type resizeResult struct {
	Source  string         `json:"source"`
	Format  string         `json:"format"`
	Outputs []resizedImage `json:"outputs"`
}

type resizedImage struct {
	Path   string `json:"path"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// ImageResize scales an image to each of the requested widths and writes the
// results under dataDir. Files are named after the source, so a retry or a
// repeat overwrites the same files instead of piling up new ones.
func ImageResize(dataDir string) Handler {
	return func(ctx context.Context, payload json.RawMessage, progress Progress) (any, error) {
		var p struct {
			Source string `json:"source"`
			Sizes  []int  `json:"sizes"`
			Format string `json:"format"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, Permanent(fmt.Errorf("image.resize: bad payload: %w", err))
		}
		format, err := outputFormat(p.Format)
		if err != nil {
			return nil, Permanent(err)
		}
		if len(p.Sizes) == 0 || len(p.Sizes) > maxSizes {
			return nil, Permanent(fmt.Errorf("image.resize: sizes needs 1 to %d widths", maxSizes))
		}
		for _, w := range p.Sizes {
			if w < 1 || w > maxWidth {
				return nil, Permanent(fmt.Errorf("image.resize: width %d is not between 1 and %d", w, maxWidth))
			}
		}

		src, err := loadSource(ctx, p.Source)
		if err != nil {
			return nil, err
		}

		// Enlarging only blurs, so widths past the source's are capped at it.
		srcW, srcH := src.Bounds().Dx(), src.Bounds().Dy()
		var widths []int
		for _, w := range p.Sizes {
			widths = append(widths, min(w, srcW))
		}
		slices.Sort(widths)
		widths = slices.Compact(widths)

		sum := sha256.Sum256([]byte(p.Source))
		dir := filepath.Join(dataDir, "images", hex.EncodeToString(sum[:8]))
		res := resizeResult{Source: p.Source, Format: format}
		for i, w := range widths {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			h := max(1, int(math.Round(float64(srcH)*float64(w)/float64(srcW))))
			out := src
			if w != srcW {
				dst := image.NewRGBA(image.Rect(0, 0, w, h))
				draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
				out = dst
			}
			path := filepath.Join(dir, fmt.Sprintf("w%d.%s", w, format))
			if err := writeImage(path, out, format); err != nil {
				return nil, fmt.Errorf("image.resize: %w", err)
			}
			res.Outputs = append(res.Outputs, resizedImage{Path: path, Width: w, Height: h})
			progress((i + 1) * 100 / len(widths))
		}
		return res, nil
	}
}

func outputFormat(f string) (string, error) {
	switch strings.ToLower(f) {
	case "", "jpeg", "jpg":
		return "jpeg", nil
	case "png":
		return "png", nil
	case "webp":
		// x/image can only decode WebP. The result says what was written.
		return "jpeg", nil
	}
	return "", fmt.Errorf("image.resize: format %q is not one of jpeg, png or webp", f)
}

// loadSource fetches an http(s) source. Anything else, such as the dashboard's
// s3:// flood payload, gets a built-in sample, so demos need no network.
func loadSource(ctx context.Context, source string) (image.Image, error) {
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		return sample(source), nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, Permanent(fmt.Errorf("image.resize: %w", err))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("image.resize: fetch source: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("image.resize: fetch source: got %s", resp.Status)
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return nil, err
		}
		return nil, Permanent(err)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("image.resize: fetch source: %w", err)
	}
	if len(data) > maxSourceBytes {
		return nil, Permanent(fmt.Errorf("image.resize: source is over %d MB", maxSourceBytes>>20))
	}
	// Checking the header first stops a small file from claiming a huge canvas.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, Permanent(fmt.Errorf("image.resize: source is not an image: %w", err))
	}
	if cfg.Width*cfg.Height > maxSourcePixels {
		return nil, Permanent(fmt.Errorf("image.resize: source is %dx%d, too large", cfg.Width, cfg.Height))
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, Permanent(fmt.Errorf("image.resize: decode source: %w", err))
	}
	return img, nil
}

// writeImage writes through a temp file and a rename, so two jobs writing the
// same output never leave a half-written file behind.
func writeImage(path string, img image.Image, format string) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(f.Name())
		}
	}()

	if format == "png" {
		err = png.Encode(f, img)
	} else {
		err = jpeg.Encode(f, img, &jpeg.Options{Quality: 85})
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// The samples are drawn once and only read after that, so jobs can share them.
var samples = map[string]func() image.Image{
	"gradient": sync.OnceValue(func() image.Image { return drawSample(gradientAt) }),
	"checker":  sync.OnceValue(func() image.Image { return drawSample(checkerAt) }),
	"rings":    sync.OnceValue(func() image.Image { return drawSample(ringsAt) }),
}

// sample returns the built-in image named by "sample:<name>", or one picked by
// hashing the source.
func sample(source string) image.Image {
	if gen, ok := samples[strings.TrimPrefix(source, "sample:")]; ok {
		return gen()
	}
	names := slices.Sorted(maps.Keys(samples))
	h := fnv.New32a()
	h.Write([]byte(source))
	return samples[names[h.Sum32()%uint32(len(names))]]()
}

const sampleW, sampleH = 640, 480

func drawSample(at func(x, y int) color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, sampleW, sampleH))
	for y := range sampleH {
		for x := range sampleW {
			img.SetRGBA(x, y, at(x, y))
		}
	}
	return img
}

func gradientAt(x, y int) color.RGBA {
	return color.RGBA{uint8(x * 255 / sampleW), uint8(y * 255 / sampleH), 160, 255}
}

func checkerAt(x, y int) color.RGBA {
	if (x/40+y/40)%2 == 0 {
		return color.RGBA{240, 240, 235, 255}
	}
	return color.RGBA{40, uint8(80 + y*120/sampleH), 120, 255}
}

func ringsAt(x, y int) color.RGBA {
	d := math.Hypot(float64(x-sampleW/2), float64(y-sampleH/2))
	v := uint8(128 + 127*math.Sin(d/12))
	return color.RGBA{v, 255 - v, 200, 255}
}
