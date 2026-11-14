package dockertest

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
)

// SampleStats is what every stats call returns.
var SampleStats = docker.Stats{
	CPUTotal: 2000, CPUSystem: 20000, PreCPUTotal: 1000, PreCPUSystem: 10000,
	OnlineCPUs: 4, MemoryUsage: 500, MemoryLimit: 1000, MemoryInactive: 100,
}

func reply(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}

// PortInUseMessage is Docker's start error when a published host port is
// already bound (internal/game/testdata/docker/port-in-use.log): name is
// the container, endpoint the 64-digit ID of its network endpoint, port the
// host port.
func PortInUseMessage(name, endpoint, port string) string {
	return "failed to set up container networking: driver failed programming external connectivity on endpoint " +
		name + " (" + endpoint + "): Bind for 0.0.0.0:" + port + " failed: port is already allocated"
}

// endpointID is the fake's network endpoint ID for a container: 64 hex
// digits, as Docker's are.
func endpointID(containerID string) string {
	sum := sha256.Sum256([]byte("endpoint/" + containerID))
	return hex.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// Everything below that takes no lock is called with d.mu held.

func (d *Daemon) newID() string {
	d.serial++
	return fmt.Sprintf("%064x", d.serial)
}

func (d *Daemon) broadcast() {
	close(d.changed)
	d.changed = make(chan struct{})
}

// find resolves ref as Docker does: a name or a full ID first, then an ID
// prefix that matches one container only.
func (d *Daemon) find(ref string) *Container {
	for _, c := range d.containers {
		if c.ID == ref || c.Name == ref {
			return c
		}
	}
	var match *Container
	for _, c := range d.containers {
		if ref != "" && strings.HasPrefix(c.ID, ref) {
			if match != nil {
				return nil
			}
			match = c
		}
	}
	return match
}

// joinedContainer returns the target of a "container:<ref>" network mode.
func joinedContainer(mode string) (string, bool) {
	return strings.CutPrefix(mode, "container:")
}

func (d *Daemon) ipFor(c *Container) string {
	for i, x := range d.containers {
		if x == c {
			return fmt.Sprintf("172.18.0.%d", i+2)
		}
	}
	return ""
}

func (d *Daemon) markRunning(c *Container) {
	c.Status = "running"
	c.ExitCode, c.OOMKilled, c.Error = 0, false, ""
	c.StartedAt, c.FinishedAt = time.Now().UTC(), time.Time{}
	mode := c.Spec.HostConfig.NetworkMode
	if _, shared := joinedContainer(mode); shared {
		c.Networks = nil
	} else {
		if len(c.Networks) == 0 {
			if mode == "" {
				mode = "bridge"
			}
			c.Networks = map[string]string{mode: ""}
		}
		for name := range c.Networks {
			if addr := c.seeded[name]; addr != "" {
				c.Networks[name] = addr
			} else {
				c.Networks[name] = d.ipFor(c)
			}
		}
	}
	d.broadcast()
}

func (d *Daemon) markExited(c *Container, code int) {
	c.Status = "exited"
	c.ExitCode = code
	c.FinishedAt = time.Now().UTC()
	for name := range c.Networks {
		c.Networks[name] = ""
	}
	d.broadcast()
}

func (d *Daemon) list(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "true"
	var filters map[string][]string
	if f := r.URL.Query().Get("filters"); f != "" {
		if err := json.Unmarshal([]byte(f), &filters); err != nil {
			reply(w, http.StatusBadRequest, "bad filters")
			return
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []docker.ContainerSummary{}
	for _, c := range d.containers {
		if (!all && c.Status != "running") || !hasLabels(c.Spec.Labels, filters["label"]) || !onNetworks(c, filters["network"]) {
			continue
		}
		out = append(out, docker.ContainerSummary{
			ID: c.ID, Names: []string{"/" + c.Name}, Image: c.Spec.Image,
			State: c.Status, Labels: c.Spec.Labels,
		})
	}
	writeJSON(w, out)
}

// onNetworks reports whether c is attached to one of names, as Docker's
// network filter does; a container never started counts by its network mode.
func onNetworks(c *Container, names []string) bool {
	if len(names) == 0 {
		return true
	}
	for _, n := range names {
		if _, ok := c.Networks[n]; ok {
			return true
		}
		if len(c.Networks) == 0 && n == c.Spec.HostConfig.NetworkMode {
			return true
		}
	}
	return false
}

// hasLabels reports whether labels satisfies every "key" or "key=value" want.
func hasLabels(labels map[string]string, want []string) bool {
	for _, w := range want {
		k, v, hasValue := strings.Cut(w, "=")
		got, ok := labels[k]
		if !ok || (hasValue && got != v) {
			return false
		}
	}
	return true
}

func (d *Daemon) create(w http.ResponseWriter, r *http.Request) {
	var spec docker.ContainerSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		reply(w, http.StatusBadRequest, "bad request body")
		return
	}
	if msg := invalidNetworkMode(spec); msg != "" {
		reply(w, http.StatusBadRequest, msg)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	name := r.URL.Query().Get("name")
	if name == "" {
		name = fmt.Sprintf("fake_%d", d.serial+1)
	}
	if other := d.find(name); other != nil {
		reply(w, http.StatusConflict, fmt.Sprintf(
			"Conflict. The container name \"/%s\" is already in use by container \"%s\". "+
				"You have to remove (or rename) that container to be able to reuse that name.", name, other.ID))
		return
	}
	if !d.images[spec.Image] {
		reply(w, http.StatusNotFound, "No such image: "+spec.Image)
		return
	}
	c := &Container{ID: d.newID(), Name: name, Spec: spec, Status: "created"}
	for _, b := range spec.HostConfig.Binds {
		parts := strings.Split(b, ":")
		if len(parts) >= 2 {
			c.Mounts = append(c.Mounts, docker.Mount{Source: parts[0], Destination: parts[1]})
		}
	}
	d.containers = append(d.containers, c)
	d.broadcast()
	if f, failing := d.callFail["POST create"]; failing {
		// The container exists but the caller is told it failed, as when a
		// connection drops after the daemon has made it.
		reply(w, f.status, f.msg)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{"Id": c.ID, "Warnings": []string{}})
}

// invalidNetworkMode returns Docker's reason for refusing a create that joins
// another container's network with options that belong to a network of its
// own, or "" when the spec is fine. The target is checked at start, not here.
func invalidNetworkMode(spec docker.ContainerSpec) string {
	target, shared := joinedContainer(spec.HostConfig.NetworkMode)
	switch {
	case !shared:
		return ""
	case target == "":
		return "invalid container format container:<name|id>"
	case len(spec.HostConfig.PortBindings) > 0:
		return "conflicting options: port publishing and the container type network mode"
	case len(spec.ExposedPorts) > 0:
		return "conflicting options: port exposing and the container type network mode"
	}
	return ""
}

// joinFailure returns why c cannot start in the network of the container its
// mode names, or "" when it can. Docker looks the target up at start, so a
// name that now means a replacement is found and an old ID is not.
func (d *Daemon) joinFailure(c *Container) (status int, msg string) {
	ref, shared := joinedContainer(c.Spec.HostConfig.NetworkMode)
	if !shared {
		return 0, ""
	}
	switch target := d.find(ref); {
	case target == nil:
		return http.StatusNotFound, "No such container: " + ref
	case target.Status == "restarting":
		return http.StatusConflict, "Container " + target.ID + " is restarting, wait until the container is running"
	case target.Status != "running":
		return http.StatusConflict, "cannot join network namespace of a non running container: " + ref
	}
	return 0, ""
}

func (d *Daemon) pull(w http.ResponseWriter, r *http.Request) {
	ref := r.URL.Query().Get("fromImage")
	if ref == "" {
		reply(w, http.StatusBadRequest, "fromImage is required")
		return
	}
	d.mu.Lock()
	d.pulls = append(d.pulls, ref)
	msg, fail := d.pullFail[ref]
	if !fail {
		d.images[ref] = true
	}
	d.mu.Unlock()
	enc := json.NewEncoder(w)
	_ = enc.Encode(map[string]string{"status": "Pulling from " + ref})
	if fail {
		_ = enc.Encode(map[string]any{"error": msg, "errorDetail": map[string]string{"message": msg}})
		return
	}
	_ = enc.Encode(map[string]string{"status": "Status: Downloaded newer image for " + ref})
}

func (d *Daemon) inspectImage(w http.ResponseWriter, ref string) {
	d.mu.Lock()
	present := d.images[ref]
	d.mu.Unlock()
	if !present {
		reply(w, http.StatusNotFound, "No such image: "+ref)
		return
	}
	writeJSON(w, map[string]any{
		"Id":          "sha256:" + strings.Repeat("ab", 32),
		"RepoTags":    []string{ref},
		"RepoDigests": []string{},
	})
}

func (d *Daemon) remove(w http.ResponseWriter, r *http.Request, ref string) {
	force := r.URL.Query().Get("force") == "true"
	d.mu.Lock()
	defer d.mu.Unlock()
	c := d.find(ref)
	if c == nil {
		reply(w, http.StatusNotFound, "No such container: "+ref)
		return
	}
	if c.Status == "running" && !force {
		reply(w, http.StatusConflict, "cannot remove container "+c.Name+": container is running")
		return
	}
	for i, x := range d.containers {
		if x == c {
			d.containers = append(d.containers[:i], d.containers[i+1:]...)
			break
		}
	}
	d.broadcast()
	w.WriteHeader(http.StatusNoContent)
}

func (d *Daemon) containerCall(w http.ResponseWriter, r *http.Request, ref, action string) {
	if action == "logs" {
		d.logs(w, r, ref)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	c := d.find(ref)
	if c == nil {
		reply(w, http.StatusNotFound, "No such container: "+ref)
		return
	}
	switch action {
	case "json":
		writeJSON(w, inspectBody(c))
	case "stats":
		writeJSON(w, statsBody())
	case "start":
		if c.Status == "running" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if status, msg := d.joinFailure(c); status != 0 {
			// Docker leaves the container created, with the reason in State.Error.
			c.Error, c.ExitCode = msg, 128
			d.broadcast()
			reply(w, status, msg)
			return
		}
		if port, armed := d.portInUse[c.Name]; armed {
			delete(d.portInUse, c.Name)
			c.Error = PortInUseMessage(c.Name, endpointID(c.ID), port)
			c.ExitCode = 128
			d.broadcast()
			reply(w, http.StatusInternalServerError, c.Error)
			return
		}
		d.markRunning(c)
		w.WriteHeader(http.StatusNoContent)
	case "stop":
		if c.Status == "restarting" {
			// The stop cancels the pending restart.
			c.Status = "exited"
			d.broadcast()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if c.Status != "running" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		d.markExited(c, 0)
		w.WriteHeader(http.StatusNoContent)
	case "kill":
		if c.Status != "running" {
			reply(w, http.StatusConflict, "Cannot kill container: "+c.Name+": container "+c.ID+" is not running")
			return
		}
		d.markExited(c, 137)
		w.WriteHeader(http.StatusNoContent)
	default:
		reply(w, http.StatusNotFound, "page not found")
	}
}

func inspectBody(c *Container) map[string]any {
	nets := map[string]any{}
	for name, addr := range c.Networks {
		nets[name] = networkBody(addr)
	}
	mode := c.Spec.HostConfig.NetworkMode
	if mode == "" {
		mode = "default"
	}
	return map[string]any{
		"Id":           c.ID,
		"Name":         "/" + c.Name,
		"RestartCount": c.RestartCount,
		"State": map[string]any{
			"Status": c.Status, "Running": c.Status == "running",
			"ExitCode": c.ExitCode, "OOMKilled": c.OOMKilled, "Error": c.Error,
			"StartedAt": c.StartedAt, "FinishedAt": c.FinishedAt,
		},
		"Config": map[string]any{
			"Image": c.Spec.Image, "Labels": c.Spec.Labels, "Env": c.Spec.Env,
		},
		"HostConfig":      map[string]any{"NetworkMode": mode},
		"Mounts":          c.Mounts,
		"NetworkSettings": map[string]any{"Networks": nets, "Ports": portsBody(c)},
	}
}

// networkBody is one network's entry in an inspect reply, from an address in
// the form Container.Networks describes.
func networkBody(addr string) map[string]any {
	body := map[string]any{"IPAddress": "", "IPPrefixLen": 0, "GlobalIPv6Address": "", "GlobalIPv6PrefixLen": 0}
	for _, one := range strings.Split(addr, ",") {
		var ip netip.Addr
		bits := 16 // a bare address has the prefix length Docker's default network has
		if p, err := netip.ParsePrefix(one); err == nil {
			ip, bits = p.Addr(), p.Bits()
		} else if a, err := netip.ParseAddr(one); err == nil {
			ip = a
			if a.Is6() {
				bits = 64
			}
		} else {
			continue
		}
		if ip.Is6() {
			body["GlobalIPv6Address"], body["GlobalIPv6PrefixLen"] = ip.String(), bits
		} else {
			body["IPAddress"], body["IPPrefixLen"] = ip.String(), bits
		}
	}
	return body
}

// portsBody is NetworkSettings.Ports: filled in only while the container
// runs, with an exposed but unpublished port as null, and a port published on
// every address listed for IPv4 and IPv6, as Docker does.
func portsBody(c *Container) map[string]any {
	ports := map[string]any{}
	if c.Status != "running" {
		return ports
	}
	for port := range c.Spec.ExposedPorts {
		ports[port] = nil
	}
	for port, bindings := range c.Spec.HostConfig.PortBindings {
		var out []docker.PortBinding
		for _, b := range bindings {
			if b.HostIP == "" {
				out = append(out, docker.PortBinding{HostIP: "0.0.0.0", HostPort: b.HostPort}, docker.PortBinding{HostIP: "::", HostPort: b.HostPort})
			} else {
				out = append(out, b)
			}
		}
		ports[port] = out
	}
	return ports
}

func statsBody() map[string]any {
	s := SampleStats
	cpu := func(total, system uint64) map[string]any {
		return map[string]any{
			"cpu_usage":        map[string]any{"total_usage": total},
			"system_cpu_usage": system, "online_cpus": s.OnlineCPUs,
		}
	}
	return map[string]any{
		"cpu_stats":    cpu(s.CPUTotal, s.CPUSystem),
		"precpu_stats": cpu(s.PreCPUTotal, s.PreCPUSystem),
		"memory_stats": map[string]any{
			"usage": s.MemoryUsage, "limit": s.MemoryLimit,
			"stats": map[string]any{"inactive_file": s.MemoryInactive},
		},
	}
}

func (d *Daemon) logs(w http.ResponseWriter, r *http.Request, ref string) {
	q := r.URL.Query()
	follow := q.Get("follow") == "true"
	stamps := q.Get("timestamps") == "1" || q.Get("timestamps") == "true"
	var since time.Time
	if v := q.Get("since"); v != "" {
		secs, frac, _ := strings.Cut(v, ".")
		s, err1 := strconv.ParseInt(secs, 10, 64)
		ns, err2 := strconv.ParseInt((frac + "000000000")[:9], 10, 64)
		if err1 != nil || err2 != nil {
			reply(w, http.StatusBadRequest, "bad since: "+v)
			return
		}
		since = time.Unix(s, ns)
	}
	d.mu.Lock()
	c := d.find(ref)
	if c == nil {
		d.mu.Unlock()
		reply(w, http.StatusNotFound, "No such container: "+ref)
		return
	}
	next := 0
	if n, err := strconv.Atoi(q.Get("tail")); err == nil && n < len(c.Log) {
		next = len(c.Log) - n
	}
	d.mu.Unlock()

	w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
	w.WriteHeader(http.StatusOK)
	flush(w)
	for {
		d.mu.Lock()
		c := d.find(ref)
		if c == nil {
			d.mu.Unlock()
			return
		}
		next = min(next, len(c.Log)) // DropLog may have emptied it
		entries := append([]LogEntry(nil), c.Log[next:]...)
		running := c.Status == "running"
		changed := d.changed
		if follow && d.holdFollow {
			entries, running = nil, true
		}
		if !follow && d.cutReads > 0 && len(entries) > d.cutReads {
			entries = entries[:d.cutReads]
		}
		d.mu.Unlock()

		for _, e := range entries {
			// Docker keeps a line written at the since time itself.
			if e.Time.Before(since) {
				continue
			}
			text := e.Text
			if stamps {
				text = e.Time.UTC().Format(dockerTime) + " " + text
			}
			var hdr [8]byte
			hdr[0] = byte(e.Stream)
			binary.BigEndian.PutUint32(hdr[4:], uint32(len(text)+1)) //nolint:gosec // a test line is short
			_, _ = w.Write(hdr[:])
			_, _ = w.Write([]byte(text + "\n"))
		}
		next += len(entries)
		flush(w)
		if !follow || !running {
			return
		}
		select {
		case <-changed:
		case <-r.Context().Done():
			return
		}
	}
}

// dockerTime is how Docker writes a log line's timestamp: RFC 3339 with
// all nine digits of the nanoseconds.
const dockerTime = "2006-01-02T15:04:05.000000000Z07:00"

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
