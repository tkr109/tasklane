package drivers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tkr109/tasklane/config"
)

// apiVersion is old enough for any recent Docker and has every field used here.
const apiVersion = "v1.41"

// roleLabel marks the containers the scaler owns, so it finds them again after a
// restart, and make down can remove them.
const roleLabel = "tasklane.role"

// Docker runs workers as containers, through the Docker Engine API on its unix
// socket. It needs five plain HTTP calls, so it does without the Docker SDK.
type Docker struct {
	http        *http.Client
	cfg         config.Docker
	stopTimeout time.Duration
}

// NewDocker returns a driver that gives a stopping worker stopTimeout to drain
// before Docker kills it.
func NewDocker(cfg config.Docker, stopTimeout time.Duration) *Docker {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", cfg.Socket)
		},
	}
	return &Docker{http: &http.Client{Transport: transport}, cfg: cfg, stopTimeout: stopTimeout}
}

// Ping checks that the daemon answers, so a missing socket shows at startup.
func (d *Docker) Ping(ctx context.Context) error {
	return d.call(ctx, http.MethodGet, "/_ping", nil, nil)
}

func (d *Docker) List(ctx context.Context) ([]Instance, error) {
	filters, err := json.Marshal(map[string][]string{"label": {roleLabel + "=worker"}})
	if err != nil {
		return nil, err
	}
	q := url.Values{"all": {"true"}, "filters": {string(filters)}}
	var containers []struct {
		Names   []string
		State   string
		Status  string
		Created int64
	}
	if err := d.call(ctx, http.MethodGet, "/containers/json?"+q.Encode(), nil, &containers); err != nil {
		return nil, err
	}
	list := make([]Instance, 0, len(containers))
	for _, c := range containers {
		if len(c.Names) == 0 {
			continue
		}
		list = append(list, Instance{
			Name:    strings.TrimPrefix(c.Names[0], "/"),
			Running: c.State == "running",
			Status:  c.Status,
			Created: time.Unix(c.Created, 0),
		})
	}
	return list, nil
}

type containerSpec struct {
	Image       string
	Cmd         []string
	Hostname    string
	Env         []string
	Labels      map[string]string
	StopTimeout int
	HostConfig  hostConfig
}

type hostConfig struct {
	NetworkMode string   `json:",omitempty"`
	Binds       []string `json:",omitempty"`
}

func (d *Docker) Start(ctx context.Context, name string) error {
	spec := containerSpec{
		Image:       d.cfg.Image,
		Cmd:         []string{"worker"},
		Hostname:    name,
		Env:         append(slices.Clip(d.cfg.Env), "WORKER_ID="+name),
		Labels:      map[string]string{roleLabel: "worker"},
		StopTimeout: int(d.stopTimeout.Seconds()),
		HostConfig:  hostConfig{NetworkMode: d.cfg.Network, Binds: d.cfg.Volumes},
	}
	q := url.Values{"name": {name}}
	if err := d.call(ctx, http.MethodPost, "/containers/create?"+q.Encode(), spec, nil); err != nil {
		return err
	}
	if err := d.call(ctx, http.MethodPost, "/containers/"+url.PathEscape(name)+"/start", nil, nil); err != nil {
		return errors.Join(err, d.remove(context.WithoutCancel(ctx), name))
	}
	return nil
}

// Stop sends the worker SIGTERM, which makes it drain, and waits up to the stop
// timeout before Docker kills it. Then it removes the container.
func (d *Docker) Stop(ctx context.Context, name string) error {
	q := url.Values{"t": {strconv.Itoa(int(d.stopTimeout.Seconds()))}}
	err := d.call(ctx, http.MethodPost, "/containers/"+url.PathEscape(name)+"/stop?"+q.Encode(), nil, nil)
	if err != nil && !hasStatus(err, http.StatusNotFound) {
		return err
	}
	return d.remove(ctx, name)
}

func (d *Docker) remove(ctx context.Context, name string) error {
	err := d.call(ctx, http.MethodDelete, "/containers/"+url.PathEscape(name)+"?force=true", nil, nil)
	// Gone already, or someone else is removing it.
	if hasStatus(err, http.StatusNotFound) || hasStatus(err, http.StatusConflict) {
		return nil
	}
	return err
}

type apiError struct {
	call    string
	status  int
	message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("docker %s: %d %s", e.call, e.status, e.message)
}

func hasStatus(err error, status int) bool {
	var e *apiError
	return errors.As(err, &e) && e.status == status
}

func (d *Docker) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker/"+apiVersion+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}
	defer resp.Body.Close()

	// 304 is what stopping a container that has already stopped returns.
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotModified {
		var e struct{ Message string }
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
		call, _, _ := strings.Cut(path, "?")
		return &apiError{call: method + " " + call, status: resp.StatusCode, message: e.Message}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
