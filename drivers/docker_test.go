package drivers

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/tkr109/tasklane/config"
)

// fakeDaemon answers the Docker API calls the driver makes, on a unix socket.
type fakeDaemon struct {
	mu         sync.Mutex
	states     map[string]string
	specs      map[string]containerSpec
	stopWaits  []string
	failStarts bool
}

func startFakeDaemon(t *testing.T) (*fakeDaemon, string) {
	t.Helper()
	// Socket paths are short on macOS, too short for t.TempDir.
	dir, err := os.MkdirTemp("", "dockerd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "docker.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}

	f := &fakeDaemon{states: map[string]string{}, specs: map[string]containerSpec{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1.41/_ping", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("GET /v1.41/containers/json", f.list)
	mux.HandleFunc("POST /v1.41/containers/create", f.create)
	mux.HandleFunc("POST /v1.41/containers/{name}/start", f.start)
	mux.HandleFunc("POST /v1.41/containers/{name}/stop", f.stop)
	mux.HandleFunc("DELETE /v1.41/containers/{name}", f.remove)

	srv := httptest.NewUnstartedServer(mux)
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return f, socket
}

func (f *fakeDaemon) list(w http.ResponseWriter, r *http.Request) {
	var filters map[string][]string
	if err := json.Unmarshal([]byte(r.URL.Query().Get("filters")), &filters); err != nil ||
		!slices.Equal(filters["label"], []string{"tasklane.role=worker"}) || r.URL.Query().Get("all") != "true" {
		http.Error(w, `{"message":"want every container with the worker label"}`, http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	list := []map[string]any{}
	for name, state := range f.states {
		list = append(list, map[string]any{"Names": []string{"/" + name}, "State": state, "Status": state, "Created": 1700000000})
	}
	json.NewEncoder(w).Encode(list)
}

func (f *fakeDaemon) create(w http.ResponseWriter, r *http.Request) {
	var spec containerSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		http.Error(w, `{"message":"bad spec"}`, http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	name := r.URL.Query().Get("name")
	f.states[name], f.specs[name] = "created", spec
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(`{"Id":"abc"}`))
}

func (f *fakeDaemon) start(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failStarts {
		http.Error(w, `{"message":"no such image"}`, http.StatusInternalServerError)
		return
	}
	f.states[r.PathValue("name")] = "running"
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeDaemon) stop(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := r.PathValue("name")
	f.stopWaits = append(f.stopWaits, r.URL.Query().Get("t"))
	switch f.states[name] {
	case "":
		http.Error(w, `{"message":"No such container"}`, http.StatusNotFound)
	case "running":
		f.states[name] = "exited"
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotModified)
	}
}

func (f *fakeDaemon) remove(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := r.PathValue("name")
	if _, ok := f.states[name]; !ok || r.URL.Query().Get("force") != "true" {
		http.Error(w, `{"message":"No such container"}`, http.StatusNotFound)
		return
	}
	delete(f.states, name)
	w.WriteHeader(http.StatusNoContent)
}

func newTestDocker(socket string) *Docker {
	return NewDocker(config.Docker{
		Socket:  socket,
		Image:   "tasklane:dev",
		Network: "tasklane_default",
		Volumes: []string{"tasklane_jobdata:/data"},
		Env:     []string{"DATABASE_URL=postgres://postgres/tasklane"},
	}, 40*time.Second)
}

func TestDockerStartsListsAndStopsWorkers(t *testing.T) {
	f, socket := startFakeDaemon(t)
	d := newTestDocker(socket)
	ctx := context.Background()

	if err := d.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.Start(ctx, "tasklane-worker-1"); err != nil {
		t.Fatal(err)
	}
	spec := f.specs["tasklane-worker-1"]
	if spec.Image != "tasklane:dev" || !slices.Equal(spec.Cmd, []string{"worker"}) || spec.Hostname != "tasklane-worker-1" ||
		spec.Labels["tasklane.role"] != "worker" || spec.StopTimeout != 40 {
		t.Errorf("container spec %+v", spec)
	}
	if !slices.Equal(spec.Env, []string{"DATABASE_URL=postgres://postgres/tasklane", "WORKER_ID=tasklane-worker-1"}) {
		t.Errorf("env %v", spec.Env)
	}
	if spec.HostConfig.NetworkMode != "tasklane_default" || !slices.Equal(spec.HostConfig.Binds, []string{"tasklane_jobdata:/data"}) {
		t.Errorf("host config %+v", spec.HostConfig)
	}

	list, err := d.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "tasklane-worker-1" || !list[0].Running || list[0].Created.Unix() != 1700000000 {
		t.Fatalf("list %+v", list)
	}

	if err := d.Stop(ctx, "tasklane-worker-1"); err != nil {
		t.Fatal(err)
	}
	// Stopping one that is gone, or has exited, is not an error.
	if err := d.Stop(ctx, "tasklane-worker-1"); err != nil {
		t.Fatal(err)
	}
	if list, _ := d.List(ctx); len(list) != 0 {
		t.Fatalf("still listed after stopping: %+v", list)
	}
	if !slices.Equal(f.stopWaits, []string{"40", "40"}) {
		t.Errorf("stops waited %v seconds, want the drain grace", f.stopWaits)
	}
}

func TestDockerRemovesAWorkerThatFailedToStart(t *testing.T) {
	f, socket := startFakeDaemon(t)
	f.failStarts = true
	d := newTestDocker(socket)

	err := d.Start(context.Background(), "tasklane-worker-1")
	if !hasStatus(err, http.StatusInternalServerError) {
		t.Fatalf("start: %v, want the daemon's 500", err)
	}
	if len(f.states) != 0 {
		t.Errorf("left behind %v", f.states)
	}
}
