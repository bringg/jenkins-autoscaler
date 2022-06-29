package dispatcher

import (
	"context"
	"sync"
	"time"

	"github.com/bringg/jenkins-autoscaler/pkg/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	log "github.com/sirupsen/logrus"
)

type (
	Scalerer interface {
		Do(ctx context.Context)
		GC(ctx context.Context)
	}

	Dispatcher struct {
		done    chan struct{}
		ctx     context.Context
		mtx     sync.RWMutex
		cancel  func()
		opt     *Options
		scaler  Scalerer
		logger  *log.Entry
		metrics *Metrics
	}

	// DispatcherMetrics represents metrics associated to a dispatcher.
	Metrics struct {
		processingDuration   prometheus.Summary
		gcProcessingDuration prometheus.Summary
	}

	Options struct {
		RunInterval   fs.Duration `config:"run_interval"`
		GCRunInterval fs.Duration `config:"gc_run_interval"`
	}
)

// NewMetrics returns a new registered dispatcher Metrics.
func NewMetrics() *Metrics {
	m := Metrics{
		processingDuration: promauto.NewSummary(
			prometheus.SummaryOpts{
				Namespace: config.MetricsNamespace,
				Name:      "dispatcher_processing_duration_seconds",
				Help:      "The latency of processing of autoscaling in seconds.",
			},
		),
		gcProcessingDuration: promauto.NewSummary(
			prometheus.SummaryOpts{
				Namespace: config.MetricsNamespace,
				Name:      "dispatcher_gc_processing_duration_seconds",
				Help:      "The latency of gc processing of autoscaling in seconds.",
			},
		),
	}

	return &m
}

// New returns a new Dispatcher.
func New(s Scalerer, m configmap.Mapper, l *log.Logger, mtr *Metrics) (*Dispatcher, error) {
	opt := &Options{}
	err := config.ReadOptions(m, opt)
	if err != nil {
		return nil, err
	}

	return &Dispatcher{
		scaler:  s,
		opt:     opt,
		metrics: mtr,
		logger:  log.NewEntry(l).WithField("component", "dispatcher"),
	}, nil
}

// Run starts dispatching auto scaling.
func (d *Dispatcher) Run(ctx context.Context) {
	d.done = make(chan struct{})
	defer close(d.done)

	d.mtx.Lock()
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.mtx.Unlock()

	d.run()
}

func (d *Dispatcher) run() {
	repeat := time.NewTicker(time.Duration(d.opt.RunInterval))
	gcTicker := time.NewTicker(time.Duration(d.opt.GCRunInterval))
	defer func() {
		repeat.Stop()
		gcTicker.Stop()
	}()

	for {
		select {
		case <-repeat.C:
			t := prometheus.NewTimer(d.metrics.processingDuration)
			d.scaler.Do(d.ctx)

			t.ObserveDuration()
		case <-gcTicker.C:
			t := prometheus.NewTimer(d.metrics.gcProcessingDuration)
			d.scaler.GC(d.ctx)

			t.ObserveDuration()
		case <-d.ctx.Done():
			return
		}
	}
}

// Stop the dispatcher.
func (d *Dispatcher) Stop() {
	d.mtx.Lock()
	d.cancel()
	d.mtx.Unlock()

	<-d.done
}

func (o *Options) Name() string {
	return "dispatcher"
}
