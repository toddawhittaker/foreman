package game

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/toddawhittaker/foreman/internal/datadir"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Labels on every game container (R2). Foreman finds its containers by
// them after a restart, with no database row.
const (
	labelPrefix  = "io.github.toddawhittaker.foreman."
	LabelWorld   = labelPrefix + "world"   // the world id
	LabelInstall = labelPrefix + "install" // the host data path, so two installs never share a container
)

// The container's fixed parts (SPEC 4.3 to 4.6, R1, R14, R15, R28, R30, R33).
const (
	gameMount      = "/factorio"
	gameBinary     = "/opt/factorio/bin/x64/factorio"
	gamePort       = "34197/udp"
	gameHostPort   = "34197"
	RCONPort       = "27015"
	restartPolicy  = "unless-stopped"
	serverSettings = world.ConfigDir + "/server-settings.json"
	maxSettings    = 1 << 20
)

// startScript runs the game with Foreman's flags. It reads the RCON
// password from the world's file inside the container, so the password is
// never in the container's settings (R1). Server settings are passed when
// the world has them, as the spike's command line does.
var startScript = strings.Join([]string{
	`pw=$(cat ` + gameMount + "/" + world.RCONPasswordFile + `) || exit 1`,
	`set -- --start-server-load-latest --mod-directory ` + gameMount + "/" + world.ModsDir +
		` --rcon-port ` + RCONPort + ` --rcon-password "$pw"`,
	`if [ -f ` + gameMount + "/" + serverSettings + ` ]; then set -- "$@" --server-settings ` + gameMount + "/" + serverSettings + `; fi`,
	`exec ` + gameBinary + ` "$@"`,
}, "\n")

// ContainerName is the name of a world's game container (R2).
func ContainerName(id string) string { return "foreman-" + id }

// containerSpec builds the game container for a world (SPEC 4.3 to 4.6).
// Nothing in it comes from what an admin types, apart from the world id,
// which world.ValidID has checked.
func (c *Controller) containerSpec(id, image, network string) docker.ContainerSpec {
	grace := StopGraceSeconds
	return docker.ContainerSpec{
		Image:        image,
		Labels:       map[string]string{LabelWorld: id, LabelInstall: c.hostData},
		Entrypoint:   []string{"/bin/sh"},
		Cmd:          []string{"-c", startScript},
		User:         c.user,
		OpenStdin:    true,
		StopTimeout:  &grace,
		ExposedPorts: map[string]struct{}{gamePort: {}},
		HostConfig: docker.HostConfig{
			Binds:         []string{filepath.Join(datadir.Worlds(c.hostData), id) + ":" + gameMount},
			PortBindings:  map[string][]docker.PortBinding{gamePort: {{HostPort: gameHostPort}}},
			CapDrop:       []string{"ALL"},
			SecurityOpt:   []string{"no-new-privileges"},
			RestartPolicy: docker.RestartPolicy{Name: restartPolicy},
			LogConfig: docker.LogConfig{Type: "json-file", Config: map[string]string{
				"max-size": "10m", "max-file": "3",
			}},
			NetworkMode: network,
		},
	}
}

// ourContainer reads the world's container. found is false when there is
// none. A container with the name that Foreman did not make for this world
// and this install is ErrForeign (R2).
func (c *Controller) ourContainer(ctx context.Context, id string) (cont docker.Container, found bool, err error) {
	cont, err = c.docker.InspectContainer(ctx, ContainerName(id))
	if errors.Is(err, docker.ErrNotFound) {
		return docker.Container{}, false, nil
	}
	if err != nil {
		return docker.Container{}, false, err
	}
	if cont.Labels[LabelWorld] != id || cont.Labels[LabelInstall] != c.hostData {
		return docker.Container{}, false, ErrForeign
	}
	return cont, true, nil
}

// checkNetworkName refuses a configured network that would break SPEC 4.4
// or 14.4.
func checkNetworkName(n string) error {
	if n == "host" || n == "none" || strings.HasPrefix(n, "container:") {
		return fmt.Errorf("FOREMAN_GAME_NETWORK cannot be %q: the game needs its own place on a Docker network shared with Foreman", n)
	}
	return nil
}

// gameNetwork picks the Docker network the game joins (R3, SPEC 4.4):
// FOREMAN_GAME_NETWORK when set; else the one network Foreman's own
// container is on; else, outside a container, Docker's default bridge.
func (c *Controller) gameNetwork(ctx context.Context) (string, error) {
	if c.network != "" {
		return c.network, nil
	}
	if c.selfID == "" {
		return "bridge", nil
	}
	self, err := c.docker.InspectContainer(ctx, c.selfID)
	if errors.Is(err, docker.ErrNotFound) {
		// SelfID fell back to the hostname, and no container has it.
		return "bridge", nil
	}
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(self.Networks))
	for n := range self.Networks {
		names = append(names, n)
	}
	slices.Sort(names)
	switch len(names) {
	case 0:
		return "", errors.New("the Foreman container is on no Docker network, so the game cannot share one with it. Put Foreman on a network, or set FOREMAN_GAME_NETWORK")
	case 1:
		if err := checkNetworkName(names[0]); err != nil {
			return "", fmt.Errorf("the Foreman container uses the %q network, which the game cannot share. Put Foreman on its own network, or set FOREMAN_GAME_NETWORK", names[0])
		}
		return names[0], nil
	}
	return "", fmt.Errorf("the Foreman container is on several Docker networks (%s). Set FOREMAN_GAME_NETWORK to the one the game should join", strings.Join(names, ", "))
}

// containerIP is the game's address on its network. The container is
// created on one network only.
func containerIP(cont docker.Container) string {
	for _, n := range cont.Networks {
		if n.IPAddress != "" {
			return n.IPAddress
		}
	}
	return ""
}

// readMeta reads a world's world.json.
func (c *Controller) readMeta(id string) (world.Meta, error) {
	root, err := c.worlds.OpenWorld(id)
	if err != nil {
		return world.Meta{}, err
	}
	defer func() { _ = root.Close() }()
	return world.ReadMeta(root)
}

// imageFor returns the pinned image for a game version, by its major and
// minor number (R8).
func (c *Controller) imageFor(gameVersion string) (string, error) {
	parts := strings.SplitN(gameVersion, ".", 3)
	if len(parts) >= 2 {
		if ref, ok := c.images[parts[0]+"."+parts[1]]; ok {
			return ref, nil
		}
	}
	return "", fmt.Errorf("there is no game image for Factorio %s", gameVersion)
}

// ImageFor returns the pinned image reference a world runs, and whether
// Docker has it (R7). The server page uses it to offer the download.
func (c *Controller) ImageFor(ctx context.Context, id string) (ref string, present bool, err error) {
	meta, err := c.readMeta(id)
	if err != nil {
		return "", false, err
	}
	ref, err = c.imageFor(meta.GameVersion)
	if err != nil {
		return "", false, err
	}
	_, err = c.docker.InspectImage(ctx, ref)
	if errors.Is(err, docker.ErrNotFound) {
		return ref, false, nil
	}
	if err != nil {
		return ref, false, err
	}
	return ref, true, nil
}

// secrets returns the world's RCON password and, when its server settings
// set one, the game password: every secret the game might print
// (SPEC 8.6, 14.8). It creates the RCON password file when missing (R20).
func (c *Controller) secrets(id string) (rconPW secret.Secret, all []secret.Secret, err error) {
	rconPW, err = c.worlds.RCONPassword(id)
	if err != nil {
		return secret.Secret{}, nil, fmt.Errorf("read the world's RCON password: %w", err)
	}
	gamePW, err := c.gamePassword(id)
	if err != nil {
		return secret.Secret{}, nil, err
	}
	all = []secret.Secret{rconPW}
	if gamePW.Reveal() != "" {
		all = append(all, gamePW)
	}
	return rconPW, all, nil
}

// hasRCONPassword reports, as an error, whether the world lacks its RCON
// password file.
func (c *Controller) hasRCONPassword(id string) error {
	root, err := c.worlds.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	_, err = root.Lstat(world.RCONPasswordFile)
	return err
}

// gamePassword reads game_password from the world's server settings; it is
// empty when the file or the field is missing.
func (c *Controller) gamePassword(id string) (secret.Secret, error) {
	root, err := c.worlds.OpenWorld(id)
	if err != nil {
		return secret.Secret{}, err
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat(serverSettings)
	if errors.Is(err, fs.ErrNotExist) {
		return secret.Secret{}, nil
	}
	if err != nil {
		return secret.Secret{}, err
	}
	if !info.Mode().IsRegular() {
		return secret.Secret{}, fmt.Errorf("%s is not a regular file", path.Clean(serverSettings))
	}
	f, err := root.Open(serverSettings)
	if err != nil {
		return secret.Secret{}, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxSettings))
	if err != nil {
		return secret.Secret{}, err
	}
	var doc struct {
		GamePassword string `json:"game_password"`
	}
	// A file the game itself cannot read holds no password Foreman must hide.
	if json.Unmarshal(b, &doc) != nil {
		return secret.Secret{}, nil
	}
	return secret.New(doc.GamePassword), nil
}
