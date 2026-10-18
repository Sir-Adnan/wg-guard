package dnsrouter

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/egress/routing"
	"github.com/Sir-Adnan/wg-guard/internal/subprocess"
)

// Feeder bounds and timing.
const (
	feedQueue    = 4096
	feedBatch    = 512
	feedInterval = 200 * time.Millisecond
	// learnWait bounds how long a reply waits for its new addresses to reach
	// the kernel. Past it the reply goes out anyway: a slow nft costs the
	// first connection its rule (default route), never the DNS answer.
	learnWait     = 300 * time.Millisecond
	minTimeout    = 30 * time.Minute
	maxTimeout    = 6 * time.Hour
	maxRemembered = 65536
)

type element struct {
	set  string
	addr netip.Addr
	ttl  uint32
	// done, when set, is closed once the transaction carrying this element
	// (and every element queued before it) has finished.
	done chan struct{}
}

type memKey struct {
	set  string
	addr netip.Addr
}

// memory is what the feeder knows about one element it wrote.
type memory struct {
	refresh time.Time // write again after this (half the timeout)
	expires time.Time // the kernel drops the element at this time
}

// FeederStats are diagnostics counters.
type FeederStats struct {
	Added   uint64
	Skipped uint64
	Dropped uint64
	Failed  uint64
	// Late counts replies sent before their new addresses were written
	// (learnWait elapsed).
	Late uint64
}

// feeder batches resolved addresses into `nft -f` transactions. It keeps a
// bounded memory of recent additions so a popular name costs one element
// write per half timeout rather than one per query, and so a reply only
// waits for addresses the kernel may not have yet.
type feeder struct {
	run subprocess.Runner
	log *slog.Logger
	in  chan element
	wg  sync.WaitGroup

	mu         sync.Mutex
	sets       map[string]bool
	remembered map[memKey]memory
	// gen changes whenever memory is invalidated (forget), so a flush that
	// raced a table rebuild does not record elements the rebuild dropped.
	gen uint64

	added, skipped, dropped, failed, late atomic.Uint64
}

func newFeeder(run subprocess.Runner, log *slog.Logger) *feeder {
	return &feeder{run: run, log: log, in: make(chan element, feedQueue), sets: map[string]bool{}, remembered: map[memKey]memory{}}
}

func (f *feeder) stats() FeederStats {
	return FeederStats{Added: f.added.Load(), Skipped: f.skipped.Load(), Dropped: f.dropped.Load(), Failed: f.failed.Load(), Late: f.late.Load()}
}

// setSets replaces the live set names; memory of removed sets is dropped.
func (f *feeder) setSets(sets map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets = sets
	for k := range f.remembered {
		if !sets[k.set] {
			delete(f.remembered, k)
		}
	}
}

// forget drops the memory of the given sets: they were (re)created empty, so
// nothing the feeder wrote before is in the kernel any more.
func (f *feeder) forget(sets []string) {
	if len(sets) == 0 {
		return
	}
	drop := make(map[string]bool, len(sets))
	for _, s := range sets {
		drop[s] = true
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gen++
	for k := range f.remembered {
		if drop[k.set] {
			delete(f.remembered, k)
		}
	}
}

// learn queues the addresses of one answer for set. It returns a channel
// that closes once the addresses not known to be in the kernel have been
// written, or nil when there is nothing to wait for: every address is still
// present (a due refresh stays asynchronous), or the queue was full.
func (f *feeder) learn(set string, addrs []netip.Addr, ttls []uint32) <-chan struct{} {
	now := time.Now()
	queue := make([]element, 0, len(addrs))
	urgent := false
	f.mu.Lock()
	if !f.sets[set] {
		f.mu.Unlock()
		return nil
	}
	for i, a := range addrs {
		m, ok := f.remembered[memKey{set, a}]
		if ok && now.Before(m.refresh) {
			f.skipped.Add(1)
			continue
		}
		queue = append(queue, element{set: set, addr: a, ttl: ttls[i]})
		if !ok || !now.Before(m.expires) {
			urgent = true
		}
	}
	f.mu.Unlock()
	if len(queue) == 0 {
		return nil
	}
	var done chan struct{}
	if urgent {
		// The feeder serves its queue in order, so the last element's
		// transaction also carries the earlier ones.
		done = make(chan struct{})
		queue[len(queue)-1].done = done
	}
	for i, e := range queue {
		select {
		case f.in <- e:
		default:
			f.dropped.Add(1)
			if i == len(queue)-1 {
				return nil
			}
		}
	}
	return done
}

func (f *feeder) start(ctx context.Context) {
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		tick := time.NewTicker(feedInterval)
		defer tick.Stop()
		var batch []element
		flush := func() {
			if len(batch) > 0 {
				f.flush(ctx, batch)
				batch = batch[:0]
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-f.in:
				batch = append(batch, e)
				if e.done != nil {
					// A reply is waiting: write now, together with whatever
					// other answers are already queued.
					batch = f.drain(batch)
					flush()
				} else if len(batch) >= feedBatch {
					flush()
				}
			case <-tick.C:
				flush()
			}
		}
	}()
}

// drain moves already-queued elements into batch without blocking, up to
// feedBatch.
func (f *feeder) drain(batch []element) []element {
	for len(batch) < feedBatch {
		select {
		case e := <-f.in:
			batch = append(batch, e)
		default:
			return batch
		}
	}
	return batch
}

func (f *feeder) wait() { f.wg.Wait() }

func timeoutFor(ttl uint32) time.Duration {
	d := time.Duration(ttl) * time.Second
	return min(max(d, minTimeout), maxTimeout)
}

type pendingElement struct {
	addr    netip.Addr
	timeout time.Duration
}

func (f *feeder) flush(ctx context.Context, batch []element) {
	defer func() {
		for _, e := range batch {
			if e.done != nil {
				close(e.done)
			}
		}
	}()
	// Memory is recorded from the start time: the kernel timeout starts no
	// earlier, so the remembered expiry is never later than the real one.
	now := time.Now()
	bySet := map[string][]pendingElement{}
	f.mu.Lock()
	if len(f.remembered) > maxRemembered {
		for k, m := range f.remembered {
			if !now.Before(m.expires) {
				delete(f.remembered, k)
			}
		}
		if len(f.remembered) > maxRemembered {
			f.remembered = map[memKey]memory{}
		}
	}
	seen := map[memKey]bool{}
	for _, e := range batch {
		k := memKey{e.set, e.addr}
		if !f.sets[e.set] || seen[k] {
			continue
		}
		seen[k] = true
		if m, ok := f.remembered[k]; ok && now.Before(m.refresh) {
			f.skipped.Add(1)
			continue
		}
		bySet[e.set] = append(bySet[e.set], pendingElement{addr: e.addr, timeout: timeoutFor(e.ttl)})
	}
	gen := f.gen
	f.mu.Unlock()
	if len(bySet) == 0 || f.run == nil {
		return
	}
	sets := make([]string, 0, len(bySet))
	for s := range bySet {
		sets = append(sets, s)
	}
	sort.Strings(sets)
	scripts := make([]string, len(sets))
	var all strings.Builder
	for i, s := range sets {
		scripts[i] = setScript(s, bySet[s])
		all.WriteString(scripts[i])
	}
	failed := map[string]bool{}
	if err := f.apply(ctx, all.String()); err != nil {
		if len(sets) == 1 {
			failed[sets[0]] = true
			f.log.Warn("egress resolver: set update failed", "set", sets[0], "elements", len(bySet[sets[0]]), "err", err)
		} else {
			// One full or broken set must not stop the other rules from
			// learning: retry each set in its own transaction (at most one
			// per domain rule, egress.MaxRules).
			for i, s := range sets {
				if err := f.apply(ctx, scripts[i]); err != nil {
					failed[s] = true
					f.log.Warn("egress resolver: set update failed", "set", s, "elements", len(bySet[s]), "err", err)
				}
			}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range sets {
		n := uint64(len(bySet[s]))
		if failed[s] {
			// Not remembered, so the next answer retries it.
			f.failed.Add(n)
			continue
		}
		f.added.Add(n)
		if gen != f.gen || !f.sets[s] {
			continue
		}
		for _, e := range bySet[s] {
			f.remembered[memKey{s, e.addr}] = memory{refresh: now.Add(e.timeout / 2), expires: now.Add(e.timeout)}
		}
	}
}

// setScript writes the elements of one set with fresh timeouts. Re-adding an
// existing element does not extend its timeout on kernels without element
// timeout updates, so every write is add, delete, add in one transaction:
// the first add makes the delete valid for a new element, the delete and
// second add reset the timeout of an existing one, and the transaction is
// atomic, so a present element is never observed absent.
func setScript(set string, elems []pendingElement) string {
	t := "ip " + routing.TableName
	plain := make([]string, len(elems))
	timed := make([]string, len(elems))
	for i, e := range elems {
		plain[i] = e.addr.String()
		timed[i] = fmt.Sprintf("%s timeout %ds", e.addr, int(e.timeout.Seconds()))
	}
	list := strings.Join(plain, ", ")
	return fmt.Sprintf("add element %s %s { %s }\ndelete element %s %s { %s }\nadd element %s %s { %s }\n",
		t, set, list, t, set, list, t, set, strings.Join(timed, ", "))
}

func (f *feeder) apply(ctx context.Context, script string) error {
	tmp, err := os.CreateTemp("", "wg-guard-egress-feed-*.nft")
	if err != nil {
		return err
	}
	path := tmp.Name()
	defer os.Remove(path)
	_, werr := tmp.WriteString(script)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		return fmt.Errorf("temp script")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = f.run.Run(ctx, []string{"nft", "-f", path})
	return err
}
