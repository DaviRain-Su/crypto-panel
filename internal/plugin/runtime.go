package plugin

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MoeForge/moejs"

	"github.com/davirain-su/crypto-panel/internal/pages"
)

const (
	// memoryLimit is what one plugin's JavaScript may keep, as design §8
	// sets it: a runaway plugin is stopped, not the app.
	memoryLimit = 256 << 20
	// callTimeout bounds one hook call, including the host functions it
	// makes. A plugin that passes it is interrupted and restarted.
	callTimeout = 30 * time.Second
)

// errCallTimeout is the value the watchdog interrupts a call with.
var errCallTimeout = errors.New("plugin call timed out")

// Runner runs one plugin's module in its own moejs runtime. The module is
// compiled once; the runtime is created on first use and recreated after a
// timeout or a memory-limit stop, so a plugin that fails once is not left
// broken (design §8).
type Runner struct {
	manifest Manifest
	module   *moejs.Module
	host     *Host
	locale   string
	timeout  time.Duration

	// mu serializes calls: a moejs runtime serves one goroutine at a time.
	// rt is guarded by it; closed and restarts are atomic so the main thread
	// never waits for a call that is running.
	mu       sync.Mutex
	rt       *moejs.Runtime
	closed   atomic.Bool
	restarts atomic.Int32

	// watchMu guards the watchdog of the call in progress, which a host
	// function that waits on the user pauses.
	watchMu  sync.Mutex
	watchdog *watchdog
}

// NewRunner compiles a plugin's source. It does not run the module yet: the
// first Page call does, so a plugin that fails at load shows its error in
// the manager instead of blocking the app.
func NewRunner(m Manifest, source []byte, host *Host, locale string) (*Runner, error) {
	mod, err := moejs.Compile(m.Main, string(source))
	if err != nil {
		return nil, fmt.Errorf("plugin %s: %w", m.ID, err)
	}
	r := &Runner{
		manifest: m,
		module:   mod,
		host:     host,
		locale:   locale,
		timeout:  callTimeout,
	}
	host.SetWatchPause(r.pauseWatch, r.resumeWatch)
	return r, nil
}

// Restarts is how many times the runtime was rebuilt after a timeout or a
// memory-limit stop, for the manager.
func (r *Runner) Restarts() int { return int(r.restarts.Load()) }

// Close stops the plugin. It does not wait for a call in progress: the
// watchdog bounds that call, and the runtime becomes unreachable when it
// returns, so the main thread never blocks on a plugin.
func (r *Runner) Close() { r.closed.Store(true) }

// Page calls the plugin's page({name}) and returns the page it drew.
func (r *Runner) Page(name string) (pages.Page, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hook, err := r.module.Hook("page")
	if err != nil {
		return pages.Page{}, fmt.Errorf("plugin %s does not export page(): %w", r.manifest.ID, err)
	}
	var out pages.Page
	err = r.callLocked(func(rt *moejs.Runtime) error {
		arg, err := rt.FromGo(map[string]any{"name": name})
		if err != nil {
			return err
		}
		res, err := rt.Call(hook, arg)
		if err != nil {
			return err
		}
		if state, value, ok := moejs.PromiseResult(res); ok {
			switch state {
			case moejs.PromiseFulfilled:
				res = value
			case moejs.PromiseRejected:
				return fmt.Errorf("page(%q) rejected: %s", name, r.describe(rt, value))
			default:
				return errors.New("page() returned a promise that never settled; M1 hooks are synchronous")
			}
		}
		data, err := rt.AppendJSON(nil, res)
		if err != nil {
			return err
		}
		out, err = pages.Decode(data)
		return err
	})
	if err != nil {
		return pages.Page{}, fmt.Errorf("plugin %s: %w", r.manifest.ID, err)
	}
	return out, nil
}

// callLocked runs fn in the plugin's runtime under the watchdog, creating
// the runtime first if it does not exist. A timeout or a memory-limit stop
// drops the runtime, so the next call starts clean.
func (r *Runner) callLocked(fn func(rt *moejs.Runtime) error) error {
	if r.closed.Load() {
		return errors.New("plugin is stopped")
	}
	if r.rt == nil {
		if err := r.loadLocked(); err != nil {
			return err
		}
	}
	rt := r.rt
	err := r.watch(rt, func() error { return fn(rt) })
	if errors.Is(err, errCallTimeout) || errors.Is(err, moejs.ErrMemoryLimit) {
		r.rt = nil
		r.restarts.Add(1)
	}
	return err
}

// loadLocked creates a runtime, installs the host namespace and evaluates
// the module's top level, all under the watchdog: a plugin that loops at
// load is stopped like one that loops in a page.
func (r *Runner) loadLocked() error {
	rt := moejs.NewRuntime(moejs.Options{
		MemoryLimit:        memoryLimit,
		DisableDynamicCode: true,
		Locale:             r.locale,
	})
	err := r.watch(rt, func() error {
		if err := rt.SetGlobal("host", r.host.Namespace()); err != nil {
			return fmt.Errorf("installing host functions: %w", err)
		}
		if err := rt.Load(r.module); err != nil {
			return fmt.Errorf("loading %s: %w", r.manifest.Main, err)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errCallTimeout) {
			return fmt.Errorf("loading %s timed out after %s", r.manifest.Main, r.timeout)
		}
		return err
	}
	r.rt = rt
	return nil
}

// watch runs fn and interrupts the runtime if it passes the timeout.
func (r *Runner) watch(rt *moejs.Runtime, fn func() error) error {
	w := newWatchdog(rt, r.timeout)
	r.setWatchdog(w)
	err := fn()
	r.setWatchdog(nil)
	if w.stop() {
		return errCallTimeout
	}
	rt.ClearInterrupt()
	return err
}

// setWatchdog records the watchdog of the call in progress.
func (r *Runner) setWatchdog(w *watchdog) {
	r.watchMu.Lock()
	r.watchdog = w
	r.watchMu.Unlock()
}

// pauseWatch stops the call's watchdog while a host function waits on the
// user, so an approval may take as long as it needs.
func (r *Runner) pauseWatch() {
	r.watchMu.Lock()
	w := r.watchdog
	r.watchMu.Unlock()
	if w != nil {
		w.pause()
	}
}

// resumeWatch starts the watchdog again after pauseWatch.
func (r *Runner) resumeWatch() {
	r.watchMu.Lock()
	w := r.watchdog
	r.watchMu.Unlock()
	if w != nil {
		w.resume()
	}
}

// watchdog interrupts a runtime that runs past its timeout. A host function
// that waits on the user pauses it, so the wait is not counted as the
// plugin's time.
type watchdog struct {
	rt      *moejs.Runtime
	timeout time.Duration

	mu     sync.Mutex
	timer  *time.Timer
	paused int
	fired  bool
	done   bool
}

func newWatchdog(rt *moejs.Runtime, timeout time.Duration) *watchdog {
	w := &watchdog{rt: rt, timeout: timeout}
	w.arm()
	return w
}

// arm starts the timer; the caller holds mu, or owns w before it is shared.
func (w *watchdog) arm() {
	w.timer = time.AfterFunc(w.timeout, w.fire)
}

func (w *watchdog) fire() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done || w.fired || w.paused > 0 {
		return
	}
	w.fired = true
	w.rt.Interrupt(errCallTimeout)
}

func (w *watchdog) pause() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done || w.fired {
		return
	}
	w.paused++
	if w.paused == 1 && w.timer != nil {
		w.timer.Stop()
	}
}

func (w *watchdog) resume() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.paused == 0 {
		return
	}
	w.paused--
	if w.paused == 0 && !w.done && !w.fired {
		w.arm()
	}
}

// stop ends the watch and reports whether it fired.
func (w *watchdog) stop() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.done = true
	if w.timer != nil {
		w.timer.Stop()
	}
	return w.fired
}

// describe turns a rejected promise's reason into one line of text.
func (r *Runner) describe(rt *moejs.Runtime, v moejs.Value) string {
	s, err := rt.Realm().ToString(v)
	if err != nil {
		return "an unprintable value"
	}
	return pages.Trim(s.GoString(), 300)
}
