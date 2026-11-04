package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ContainerSpec is the body of POST /containers/create, in Docker's own
// field names. It holds only what Foreman sets; the caller decides the
// values (SPEC 4.3 to 4.6).
type ContainerSpec struct {
	Image        string              `json:"Image"`
	Labels       map[string]string   `json:"Labels,omitempty"`
	Env          []string            `json:"Env,omitempty"`
	Cmd          []string            `json:"Cmd,omitempty"`
	Entrypoint   []string            `json:"Entrypoint,omitempty"`
	User         string              `json:"User,omitempty"`
	OpenStdin    bool                `json:"OpenStdin,omitempty"`
	StopSignal   string              `json:"StopSignal,omitempty"`
	StopTimeout  *int                `json:"StopTimeout,omitempty"` // seconds
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
	HostConfig   HostConfig          `json:"HostConfig"`
}

// HostConfig is the host-side half of a ContainerSpec.
type HostConfig struct {
	Binds         []string                 `json:"Binds,omitempty"`        // "host:container[:opts]"
	PortBindings  map[string][]PortBinding `json:"PortBindings,omitempty"` // key like "34197/udp"
	CapDrop       []string                 `json:"CapDrop,omitempty"`
	SecurityOpt   []string                 `json:"SecurityOpt,omitempty"`
	RestartPolicy RestartPolicy            `json:"RestartPolicy"`
	LogConfig     LogConfig                `json:"LogConfig"`
	NetworkMode   string                   `json:"NetworkMode,omitempty"` // a network name or "bridge"
}

// PortBinding publishes a container port on the host.
type PortBinding struct {
	HostIP   string `json:"HostIp,omitempty"`
	HostPort string `json:"HostPort"`
}

// RestartPolicy is Docker's restart policy, for example "unless-stopped".
type RestartPolicy struct {
	Name string `json:"Name"`
}

// LogConfig picks the log driver and its options.
type LogConfig struct {
	Type   string            `json:"Type,omitempty"`
	Config map[string]string `json:"Config,omitempty"`
}

// CreateContainer creates a container called name from spec and returns its
// ID. A taken name is ErrConflict; a missing image is ErrNotFound.
func (c *Client) CreateContainer(ctx context.Context, name string, spec ContainerSpec) (string, error) {
	resp, err := c.do(ctx, request{
		method: http.MethodPost, path: "/containers/create",
		query: url.Values{"name": {name}}, body: spec,
	})
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		ID string `json:"Id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.ID == "" {
		return "", errors.New("the Docker daemon did not say which container it created")
	}
	return out.ID, nil
}

// StartContainer starts a container. Starting a running one is ErrNotModified.
func (c *Client) StartContainer(ctx context.Context, id string) error {
	return c.containerAction(ctx, request{method: http.MethodPost}, id, "/start")
}

// stopMargin is how long a stop may take beyond the grace period it gives
// the game: the daemon waits that long, then kills.
const stopMargin = 30 * time.Second

// StopContainer asks the container to stop, and waits up to graceSeconds
// before the daemon kills it. Stopping a stopped one is ErrNotModified.
func (c *Client) StopContainer(ctx context.Context, id string, graceSeconds int) error {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(graceSeconds)*time.Second+stopMargin)
	defer cancel()
	r := request{method: http.MethodPost, query: url.Values{"t": {strconv.Itoa(graceSeconds)}}, long: true}
	return c.containerAction(ctx, r, id, "/stop")
}

// KillContainer sends a signal (the daemon's default, SIGKILL, when signal
// is empty). Killing a container that is not running is ErrConflict.
func (c *Client) KillContainer(ctx context.Context, id, signal string) error {
	q := url.Values{}
	if signal != "" {
		q.Set("signal", signal)
	}
	return c.containerAction(ctx, request{method: http.MethodPost, query: q}, id, "/kill")
}

// RemoveContainer deletes a container. Without force, removing a running
// one is ErrConflict. Anonymous volumes are left alone.
func (c *Client) RemoveContainer(ctx context.Context, id string, force bool) error {
	r := request{method: http.MethodDelete, query: url.Values{"force": {strconv.FormatBool(force)}}}
	return c.containerAction(ctx, r, id, "")
}

// containerAction sends r to /containers/{id}{suffix} and discards the reply.
func (c *Client) containerAction(ctx context.Context, r request, id, suffix string) error {
	if err := checkID(id); err != nil {
		return err
	}
	r.path = "/containers/" + id + suffix
	resp, err := c.do(ctx, r)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func checkID(id string) error {
	if id == "" || strings.ContainsAny(id, "/?#%") || strings.Contains(id, "..") {
		return fmt.Errorf("container id %q is not valid", id)
	}
	return nil
}

// Mount is one mount of a container.
type Mount struct {
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
}

// Network is a container's place on one Docker network.
type Network struct {
	IPAddress   string
	IPPrefixLen int // the network's prefix length, as in 172.18.0.0/16
	// The IPv6 address and prefix length; empty and 0 when the network has
	// no IPv6.
	GlobalIPv6Address   string
	GlobalIPv6PrefixLen int
}

// State is a container's run state.
type State struct {
	Status     string // created, running, restarting, exited, ...
	Running    bool
	ExitCode   int
	OOMKilled  bool
	Error      string
	StartedAt  time.Time
	FinishedAt time.Time
}

// Container is the part of GET /containers/{id}/json Foreman uses.
type Container struct {
	ID           string
	Name         string // without the leading slash
	Image        string // the image reference it was created from
	Labels       map[string]string
	State        State
	RestartCount int
	Mounts       []Mount
	Networks     map[string]Network // by network name

	// Env is the raw "NAME=value" list. It can hold secrets such as the
	// RCON password, so callers pick out the names they know with EnvValue
	// and never log, show or return the list.
	Env []string
	// User is Config.User, such as "845:845"; empty when the image decides.
	User string
	// PortBindings is the configured host publishing, keyed like "27015/tcp".
	PortBindings map[string][]PortBinding
	// NetworkMode is HostConfig.NetworkMode: a network name, "bridge",
	// "host", "none", or "default" for an unset one.
	NetworkMode string
}

// EnvValue returns the value of an environment variable. When the name is
// set more than once, the last one wins, as it does in the container.
func (c Container) EnvValue(name string) (string, bool) {
	var value string
	var found bool
	for _, kv := range c.Env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == name {
			value, found = v, true
		}
	}
	return value, found
}

// PublishesTCP reports whether the container's TCP port is published on the
// host, on any host address.
func (c Container) PublishesTCP(port int) bool {
	return len(c.PortBindings[strconv.Itoa(port)+"/tcp"]) > 0
}

// InspectContainer reads one container. A missing one is ErrNotFound.
func (c *Client) InspectContainer(ctx context.Context, id string) (Container, error) {
	if err := checkID(id); err != nil {
		return Container{}, err
	}
	resp, err := c.do(ctx, request{method: http.MethodGet, path: "/containers/" + id + "/json"})
	if err != nil {
		return Container{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw struct {
		ID           string `json:"Id"`
		Name         string
		RestartCount int
		State        State
		Config       struct {
			Image  string
			Labels map[string]string
			Env    []string
			User   string
		}
		HostConfig struct {
			PortBindings map[string][]PortBinding
			NetworkMode  string
		}
		Mounts          []Mount
		NetworkSettings struct {
			Networks map[string]Network
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Container{}, fmt.Errorf("could not read container %s from the Docker daemon: %w", id, err)
	}
	return Container{
		ID:           raw.ID,
		Name:         strings.TrimPrefix(raw.Name, "/"),
		Image:        raw.Config.Image,
		Labels:       raw.Config.Labels,
		State:        raw.State,
		RestartCount: raw.RestartCount,
		Mounts:       raw.Mounts,
		Networks:     raw.NetworkSettings.Networks,
		Env:          raw.Config.Env,
		User:         raw.Config.User,
		PortBindings: raw.HostConfig.PortBindings,
		NetworkMode:  raw.HostConfig.NetworkMode,
	}, nil
}

// ContainerSummary is one row of GET /containers/json.
type ContainerSummary struct {
	ID     string `json:"Id"`
	Names  []string
	Image  string
	State  string
	Labels map[string]string
}

// ListContainers lists containers that carry every one of labels, each
// "key" or "key=value". With all false, only running ones are listed.
func (c *Client) ListContainers(ctx context.Context, all bool, labels []string) ([]ContainerSummary, error) {
	var filters map[string][]string
	if len(labels) > 0 {
		filters = map[string][]string{"label": labels}
	}
	return c.listContainers(ctx, all, filters)
}

// ListContainersOnNetwork lists every container, running or not, attached
// to the named network.
func (c *Client) ListContainersOnNetwork(ctx context.Context, network string) ([]ContainerSummary, error) {
	return c.listContainers(ctx, true, map[string][]string{"network": {network}})
}

func (c *Client) listContainers(ctx context.Context, all bool, filters map[string][]string) ([]ContainerSummary, error) {
	q := url.Values{"all": {strconv.FormatBool(all)}}
	if len(filters) > 0 {
		f, err := json.Marshal(filters)
		if err != nil {
			return nil, err
		}
		q.Set("filters", string(f))
	}
	resp, err := c.do(ctx, request{method: http.MethodGet, path: "/containers/json", query: q})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var out []ContainerSummary
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("could not read the container list from the Docker daemon: %w", err)
	}
	return out, nil
}

// Stats is one reading of a container's CPU and memory use. CPU figures are
// running totals; the caller compares them with the Pre figures for a rate.
type Stats struct {
	CPUTotal       uint64 // nanoseconds of CPU time used by the container
	CPUSystem      uint64 // nanoseconds of CPU time used by the host
	PreCPUTotal    uint64 // the same two figures one sample earlier
	PreCPUSystem   uint64
	OnlineCPUs     int
	MemoryUsage    uint64 // bytes, including page cache
	MemoryLimit    uint64
	MemoryInactive uint64 // bytes of page cache that can be dropped
}

type cpuStats struct {
	CPUUsage struct {
		TotalUsage uint64 `json:"total_usage"`
	} `json:"cpu_usage"`
	SystemCPUUsage uint64 `json:"system_cpu_usage"`
	OnlineCPUs     int    `json:"online_cpus"`
}

// ContainerStats takes one reading. It does not stream.
func (c *Client) ContainerStats(ctx context.Context, id string) (Stats, error) {
	if err := checkID(id); err != nil {
		return Stats{}, err
	}
	q := url.Values{"stream": {"false"}}
	resp, err := c.do(ctx, request{method: http.MethodGet, path: "/containers/" + id + "/stats", query: q})
	if err != nil {
		return Stats{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var raw struct {
		CPU    cpuStats `json:"cpu_stats"`
		PreCPU cpuStats `json:"precpu_stats"`
		Memory struct {
			Usage uint64            `json:"usage"`
			Limit uint64            `json:"limit"`
			Stats map[string]uint64 `json:"stats"`
		} `json:"memory_stats"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Stats{}, fmt.Errorf("could not read container %s's use from the Docker daemon: %w", id, err)
	}
	return Stats{
		CPUTotal: raw.CPU.CPUUsage.TotalUsage, CPUSystem: raw.CPU.SystemCPUUsage,
		PreCPUTotal: raw.PreCPU.CPUUsage.TotalUsage, PreCPUSystem: raw.PreCPU.SystemCPUUsage,
		OnlineCPUs:  raw.CPU.OnlineCPUs,
		MemoryUsage: raw.Memory.Usage, MemoryLimit: raw.Memory.Limit,
		MemoryInactive: raw.Memory.Stats["inactive_file"],
	}, nil
}
