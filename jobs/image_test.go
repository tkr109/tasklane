package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func resize(t *testing.T, dir, payload string) (resizeResult, []int, error) {
	t.Helper()
	var steps []int
	got, err := ImageResize(dir)(context.Background(), json.RawMessage(payload), func(p int) {
		steps = append(steps, p)
	})
	if err != nil {
		return resizeResult{}, steps, err
	}
	return got.(resizeResult), steps, nil
}

// checkFile decodes an output and compares its size with what the result claims.
func checkFile(t *testing.T, out resizedImage) {
	t.Helper()
	f, err := os.Open(out.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("%s: %v", out.Path, err)
	}
	if cfg.Width != out.Width || cfg.Height != out.Height {
		t.Errorf("%s is %dx%d, the result says %dx%d", out.Path, cfg.Width, cfg.Height, out.Width, out.Height)
	}
}

func TestResizeSample(t *testing.T) {
	res, steps, err := resize(t, t.TempDir(), `{"source": "sample:rings", "sizes": [320, 100, 2000, 320], "format": "png"}`)
	if err != nil {
		t.Fatal(err)
	}

	// The 640x480 sample is never enlarged, and repeated widths are written once.
	want := [][2]int{{100, 75}, {320, 240}, {640, 480}}
	if len(res.Outputs) != len(want) {
		t.Fatalf("outputs = %+v", res.Outputs)
	}
	for i, out := range res.Outputs {
		if out.Width != want[i][0] || out.Height != want[i][1] {
			t.Errorf("output %d is %dx%d, want %dx%d", i, out.Width, out.Height, want[i][0], want[i][1])
		}
		checkFile(t, out)
	}
	if len(steps) != 3 || steps[2] != 100 {
		t.Errorf("progress went %v", steps)
	}
}

func TestResizeFloodPayload(t *testing.T) {
	res, _, err := resize(t, t.TempDir(),
		`{"source": "s3://loadtest/uploads/flood.jpg", "sizes": [320, 640, 1280], "format": "webp"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != "jpeg" || len(res.Outputs) != 2 {
		t.Fatalf("result = %+v", res)
	}
	for _, out := range res.Outputs {
		checkFile(t, out)
	}
}

func TestResizeFromURL(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 200, 100))); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cat.png":
			w.Write(buf.Bytes())
		case "/busy.png":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/notes.txt":
			w.Write([]byte("not an image"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()

	res, _, err := resize(t, dir, `{"source": "`+srv.URL+`/cat.png", "sizes": [50]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Outputs) != 1 || res.Outputs[0].Width != 50 || res.Outputs[0].Height != 25 {
		t.Fatalf("outputs = %+v", res.Outputs)
	}
	checkFile(t, res.Outputs[0])

	if _, _, err := resize(t, dir, `{"source": "`+srv.URL+`/busy.png", "sizes": [50]}`); err == nil || IsPermanent(err) {
		t.Errorf("503: err = %v, want a retry", err)
	}
	for _, path := range []string{"/gone.png", "/notes.txt"} {
		if _, _, err := resize(t, dir, `{"source": "`+srv.URL+path+`", "sizes": [50]}`); !IsPermanent(err) {
			t.Errorf("%s: err = %v, want permanent", path, err)
		}
	}
}

func TestResizeBadPayloadIsPermanent(t *testing.T) {
	for _, payload := range []string{
		`{"source": "sample:checker"}`,
		`{"source": "sample:checker", "sizes": [0]}`,
		`{"source": "sample:checker", "sizes": [5000]}`,
		`{"source": "sample:checker", "sizes": [100], "format": "bmp"}`,
		`{"sizes": "big"}`,
	} {
		if _, _, err := resize(t, t.TempDir(), payload); !IsPermanent(err) {
			t.Errorf("%s: err = %v, want permanent", payload, err)
		}
	}
}

func BenchmarkResizeFloodPayload(b *testing.B) {
	h := ImageResize(b.TempDir())
	payload := json.RawMessage(`{"source": "s3://loadtest/uploads/flood.jpg", "sizes": [320, 640, 1280], "format": "webp"}`)
	for b.Loop() {
		if _, err := h(context.Background(), payload, func(int) {}); err != nil {
			b.Fatal(err)
		}
	}
}
