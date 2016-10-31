package alerts

import (
	"container/list"
	"log"
	"sync"
	"time"

	"golang.org/x/net/context"
)

const (
	successLabel byte = iota
	errorLabel

	defaultTriggerName     = "alerts"
	defaultWindowTime      = time.Minute
	defaultMinCount        = 10
	defaultThresholdRate   = 0.1
	defaultPollingInterval = time.Second

	triggerContextKey = "trigger"
	rateContextKey    = "rate"
)

var emptyTime = time.Time{}

type Alert struct {
	*alertOptions
}

type Trigger struct {
	*TriggerOptions

	al         *Alert
	mu         sync.RWMutex
	lst        *list.List
	lastOpened time.Time
}

type TriggerOption func(*TriggerOptions)

type TriggerOptions struct {
	// Trigger name
	Name string
	// Minimum count
	MinCount int
	// If exceed this rate, trigger fires.
	ThresholdRate float64
	// Time interval for last trigger called
	AlertInterval time.Duration
	// Time window for keeping metrics
	WindowTime time.Duration
	// If trigger fires, calls this function
	Callback Callback
	// If this is true, observer doesn't observe the metrics on background
	NoPolling bool
	// Interval time for polling status
	PollingInterval time.Duration
}

func NewTriggerOptions(opts ...TriggerOption) *TriggerOptions {
	topts := &TriggerOptions{
		Name:            defaultTriggerName,
		ThresholdRate:   defaultThresholdRate,
		WindowTime:      defaultWindowTime,
		MinCount:        defaultMinCount,
		PollingInterval: defaultPollingInterval,
	}
	for _, of := range opts {
		of(topts)
	}
	return topts
}

func newTrigger(al *Alert, opts *TriggerOptions) *Trigger {
	return &Trigger{
		TriggerOptions: opts,
		al:             al,
		lst:            list.New(),
	}
}

func WithName(name string) TriggerOption {
	return func(opts *TriggerOptions) {
		opts.Name = name
	}
}

func WithMinCount(count int) TriggerOption {
	return func(opts *TriggerOptions) {
		opts.MinCount = count
	}
}

func WithThresholdRate(rate float64) TriggerOption {
	return func(opts *TriggerOptions) {
		opts.ThresholdRate = rate
	}
}

func WithWindowTime(wt time.Duration) TriggerOption {
	return func(opts *TriggerOptions) {
		opts.WindowTime = wt
	}
}

func WithCallback(cb Callback) TriggerOption {
	return func(opts *TriggerOptions) {
		opts.Callback = cb
	}
}

func WithNoPolling(b bool) TriggerOption {
	return func(opts *TriggerOptions) {
		opts.NoPolling = b
	}
}

func WithPollingInterval(d time.Duration) TriggerOption {
	return func(opts *TriggerOptions) {
		opts.PollingInterval = d
	}
}

type status struct {
	label byte
	ts    time.Time
}

func (tg *Trigger) IncrSuccess() {
	now := time.Now()
	tg.lst.PushBack(&status{successLabel, now})
	tg.gc(now)
}

func (tg *Trigger) IncrError() {
	now := time.Now()
	tg.lst.PushBack(&status{errorLabel, now})
	tg.gc(now)
}

func (tg *Trigger) Stats() (int, int) {
	return tg.stats()
}

func (tg *Trigger) gc(now time.Time) {
	expired := now.Add(-tg.WindowTime)
	var removes []*list.Element
	for e := tg.lst.Front(); e != nil; e = e.Next() {
		st := e.Value.(*status)
		if expired.Before(st.ts) {
			break
		}
		removes = append(removes, e)
	}
	for _, e := range removes {
		tg.lst.Remove(e)
	}
}

// returns: successes, errors
func (tg *Trigger) stats() (int, int) {
	var successes, errors int
	for e := tg.lst.Front(); e != nil; e = e.Next() {
		switch e.Value.(*status).label {
		case successLabel:
			successes++
		case errorLabel:
			errors++
		}
	}
	return successes, errors
}

func (tg *Trigger) observe(ctx context.Context) {
	for {
		select {
		case now := <-time.After(tg.PollingInterval):
			tg.mu.Lock()
			tg.gc(now)
			tg.check(now)
			tg.mu.Unlock()
		case <-ctx.Done():
			return
		}
	}
}

func (tg *Trigger) check(now time.Time) {
	if tg.lastOpened == emptyTime || now.After(tg.lastOpened.Add(tg.AlertInterval)) {
		sc, er := tg.stats()
		total := sc + er
		rate := float64(er) / float64(total)
		if total >= tg.MinCount && tg.ThresholdRate <= rate {
			tg.lastOpened = now
			ctx := wrapContext(tg.al.ctx, tg, rate)
			if tg.Callback == nil {
				DefaultCallback(ctx)
			} else {
				tg.Callback(ctx)
			}
		}
	}
}

func wrapContext(ctx context.Context, tg *Trigger, rate float64) context.Context {
	return context.WithValue(
		context.WithValue(
			ctx,
			triggerContextKey,
			tg,
		),
		rateContextKey,
		rate,
	)
}

func TriggerFromContext(ctx context.Context) *Trigger {
	return ctx.Value(triggerContextKey).(*Trigger)
}

func ErrorRateFromContext(ctx context.Context) float64 {
	return ctx.Value(rateContextKey).(float64)
}

type Callback func(ctx context.Context)

func DefaultCallback(ctx context.Context) {
	log.Println("trigger fired")
}

type AlertOption func(*alertOptions)

type alertOptions struct {
	triggers    []*Trigger
	triggerOpts []*TriggerOptions
	ctx         context.Context
}

func WithTrigger(opts *TriggerOptions) AlertOption {
	return func(opt *alertOptions) {
		opt.triggerOpts = append(opt.triggerOpts, opts)
	}
}

func WithContext(ctx context.Context) AlertOption {
	return func(opt *alertOptions) {
		opt.ctx = ctx
	}
}

func NewAlert(opts ...AlertOption) *Alert {
	aopts := &alertOptions{
		ctx: context.Background(),
	}
	for _, of := range opts {
		of(aopts)
	}
	al := &Alert{
		alertOptions: aopts,
	}
	for _, opt := range al.triggerOpts {
		tg := newTrigger(al, opt)
		if !tg.NoPolling {
			go tg.observe(al.ctx)
		}
		al.triggers = append(al.triggers, tg)
	}
	return al
}

func (al *Alert) IncrSuccess() {
	for _, tg := range al.triggers {
		tg.mu.Lock()
		tg.IncrSuccess()
		tg.mu.Unlock()
	}
}

func (al *Alert) IncrError() {
	now := time.Now()
	for _, tg := range al.triggers {
		tg.mu.Lock()
		tg.IncrError()
		tg.check(now)
		tg.mu.Unlock()
	}
}

func (al *Alert) CallFunc(cb func() error) error {
	err := cb()
	if err == nil {
		al.IncrSuccess()
	} else {
		al.IncrError()
	}
	return err
}

func (al *Alert) Triggers() []*Trigger {
	return al.triggers
}
