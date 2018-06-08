// Package plugins provides support for creating extensible CLIs
package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	CmdTimeout           = 5 * time.Second
	CmdMetadataArgs      = "--plugin"
	CacheRefreshInterval = 1 * time.Hour
)

var (
	ErrPluginNotFound = errors.New("plugin not found")
	ErrPluginInvalid  = errors.New("invalid plugin")
)

// Metadata contains the metadata of a plugin.
// The only metadata that is guaranteed to be non-empty is the Path and Name. All other fields are considered optional.
type Metadata struct {
	Name       string            `json:"name"`
	Version    string            `json:"version"`
	Url        string            `json:"url"`
	Requires   map[string]string `json:"requires"`
	Aliases    []string          `json:"aliases"`
	Usage      string            `json:"usage"`
	Path       string            `json:"path"`
	ModifiedAt time.Time         `json:"modifiedAt"`
}

var DefaultManager = &Manager{
	Prefix: fmt.Sprintf("%v-", os.Args[0]),
}

func Find(pluginName string) (*Metadata, error) {
	return DefaultManager.Find(pluginName)
}

func Exec(pluginMetadata *Metadata, args []string) error {
	return DefaultManager.Exec(pluginMetadata, args)
}

func FindAll() map[string]*Metadata {
	return DefaultManager.FindAll()
}

func SetCachePath(path string) {
	DefaultManager.CachePath = path
}

func SetPrefix(prefix string) {
	DefaultManager.Prefix = prefix
}

func UpdateCacheIfStale() {
	if DefaultManager.useCache() && DefaultManager.cacheIsStale() {
		DefaultManager.FindAll()
	}
}

type Manager struct {
	Prefix     string
	Registries []string
	CachePath  string // Empty means: do not cache
	cache      map[string]*Metadata
}

// Find searches the machine for the given plugin, returning the metadata of the plugin.
// The only metadata that is guaranteed to be non-empty is the Path and Name. All other fields are considered optional.
// If found it returns the plugin, otherwise it returns ErrPluginNotFound if the plugin was not found or it returns
// ErrPluginInvalid if the plugin was found but considered unusable (e.g. not executable or invalid permissions).
func (mgr *Manager) Find(pluginName string) (*Metadata, error) {
	pluginPath, err := mgr.findPluginPath(pluginName)
	if err != nil {
		return nil, err
	}

	md, err := mgr.fetchPluginMetadata(pluginPath)
	if err != nil {
		return nil, err
	}
	return md, nil
}

// Exec executes the plugin using the provided args.
// All input and output is redirected to stdin, stdout, and stderr.
func (mgr *Manager) Exec(pluginMetadata *Metadata, args []string) error {
	// TODO remove from cache if command is in cache and could not be found!
	cmd := exec.Command(pluginMetadata.Path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// FindAll searches the machine for all plugins currently present.
func (mgr *Manager) FindAll() map[string]*Metadata {
	plugins := map[string]*Metadata{}

	dirs := strings.Split(os.Getenv("PATH"), ":")
	for _, dir := range dirs {
		fs, err := ioutil.ReadDir(dir)
		if err != nil {
			logrus.Debugf("Failed to read $PATH directory: %v", dir)
			continue
		}
		for _, f := range fs {
			if !strings.HasPrefix(f.Name(), mgr.Prefix) {
				continue
			}
			fp := path.Join(dir, f.Name())
			md, err := mgr.fetchPluginMetadata(fp)
			if err != nil {
				logrus.Debugf("%v: %v", f.Name(), err)
				continue
			}
			// TODO merge aliases
			plugins[md.Name] = md
		}
	}
	if mgr.useCache() {
		err := mgr.writeCacheAll(plugins)
		if err != nil {
			logrus.Debug("Failed to cache plugin metadata: %v", err)
		}
	}
	return plugins
}

func (mgr *Manager) findPluginPath(pluginName string) (path string, err error) {
	binaryName := mgr.binaryNameForPlugin(pluginName)
	path, err = exec.LookPath(binaryName)
	if err != nil {
		logrus.Debugf("Plugin not found on PATH: %v", err)
	}

	if len(path) == 0 {
		return "", ErrPluginNotFound
	}
	return path, nil
}

func (mgr *Manager) fetchPluginMetadata(pluginPath string) (*Metadata, error) {
	d, err := os.Stat(pluginPath)
	if err != nil {
		return nil, ErrPluginNotFound
	}
	if m := d.Mode(); m.IsDir() || m&0111 == 0 {
		return nil, ErrPluginInvalid
	}

	if mgr.useCache() && !mgr.cacheIsStale() {
		cached, err := mgr.readCache()
		if err != nil {
			logrus.Debugf("Failed to read cache for metadata fetch: %v", err)
		}
		if c, ok := cached[mgr.pluginNameFromBinary(path.Base(pluginPath))]; ok {
			if c.ModifiedAt == d.ModTime() {
				return c, nil
			} else {
				logrus.Debugf("Cache entry for %v is stale; refreshing.", pluginPath)
			}
		}
	}

	buf := bytes.NewBuffer(nil)
	ctx, cancel := context.WithTimeout(context.Background(), CmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, pluginPath, CmdMetadataArgs) // Note: issue can occur with signal propagation
	cmd.Stdout = buf
	err = cmd.Run()
	if err != nil {
		return nil, err
	}
	// Parse metadata if possible
	md := &Metadata{}
	err = json.Unmarshal(buf.Bytes(), md)
	if err != nil {
		logrus.Debugf("Failed to read plugin metadata: %v", err)
		md.Path = pluginPath
		md.Name = mgr.pluginNameFromBinary(path.Base(pluginPath))
	}
	md.ModifiedAt = d.ModTime()
	if mgr.useCache() {
		mgr.writeCache(md)
	}
	return md, nil
}

func (mgr *Manager) useCache() bool {
	return len(mgr.CachePath) > 0
}

func (mgr *Manager) cacheIsStale() bool {
	if len(mgr.CachePath) == 0 {
		return true
	}
	fi, err := os.Stat(mgr.CachePath)
	if err != nil {
		logrus.Debugf("Failed to stat cache for staleness: %v", err)
		return true
	}
	return time.Now().After(fi.ModTime().Add(CacheRefreshInterval))
}

func (mgr *Manager) readCache() (map[string]*Metadata, error) {
	if mgr.cache != nil {
		return mgr.cache, nil
	}
	cached := map[string]*Metadata{}
	if len(mgr.CachePath) == 0 {
		return cached, errors.New("undefined cache")
	}
	bs, err := ioutil.ReadFile(mgr.CachePath)
	if err != nil {
		return cached, err
	}
	err = json.Unmarshal(bs, &cached)
	if err != nil {
		return cached, err
	}
	mgr.cache = cached
	logrus.Debugf("Read plugin metadata from cache at %v", mgr.CachePath)
	return cached, nil
}

func (mgr *Manager) writeCache(md *Metadata) error {
	if md == nil {
		return errors.New("no metadata provided")
	}
	cached, err := mgr.readCache()
	if err != nil {
		return err
	}
	cached[md.Name] = md
	err = mgr.writeCacheAll(cached)
	if err != nil {
		return err
	}
	return nil
}

func (mgr *Manager) writeCacheAll(mds map[string]*Metadata) error {
	if len(mgr.CachePath) == 0 {
		return errors.New("undefined cache")
	}
	bs, err := json.Marshal(mds)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(mgr.CachePath, bs, os.ModePerm)
	if err != nil {
		return err
	}
	mgr.cache = mds
	logrus.Debugf("Cached plugin metadata in %v", mgr.CachePath)
	return nil
}

func (mgr *Manager) binaryNameForPlugin(pluginName string) string {
	return mgr.Prefix + pluginName
}

func (mgr *Manager) pluginNameFromBinary(binaryName string) string {
	return strings.TrimPrefix(binaryName, mgr.Prefix)
}
