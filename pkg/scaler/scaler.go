package scaler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/adhocore/gronx"
	"github.com/hashicorp/go-multierror"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/samber/lo"
	log "github.com/sirupsen/logrus"

	"github.com/bringg/jenkins-autoscaler/pkg/backend"
	"github.com/bringg/jenkins-autoscaler/pkg/config"
	"github.com/bringg/jenkins-autoscaler/pkg/scaler/client"
)

var ErrNodeInUse = errors.New("can't remove current node, node is in use")

type (
	Scaler struct {
		ctx           context.Context
		backend       backend.Backend
		client        client.JenkinsAccessor
		opt           *Options
		lastScaleDown time.Time
		lastScaleUp   time.Time
		logger        *log.Entry
		schedule      gronx.Gronx
		metrics       *Metrics
	}

	// Metrics represents metrics associated to a scaler.
	Metrics struct {
		numScaleUps             *prometheus.CounterVec
		numFailedScaleUps       *prometheus.CounterVec
		numScaleDowns           *prometheus.CounterVec
		numFailedScaleDowns     *prometheus.CounterVec
		numScaleToMinimum       *prometheus.CounterVec
		numFailedScaleToMinimum *prometheus.CounterVec
		numGC                   *prometheus.CounterVec
		numFailedGC             *prometheus.CounterVec
	}

	Options struct {
		DryRun                                 bool        `config:"dry_run"`
		DisableWorkingHours                    bool        `config:"disable_working_hours"`
		ControllerNodeName                     string      `config:"controller_node_name"`
		WorkingHoursCronExpressions            string      `config:"working_hours_cron_expressions"`
		MaxNodes                               int64       `config:"max_nodes"`
		MinNodesInWorkingHours                 int64       `config:"min_nodes_during_working_hours"`
		ScaleUpThreshold                       int64       `config:"scale_up_threshold"`
		ScaleDownThreshold                     int64       `config:"scale_down_threshold"`
		ScaleUpGracePeriod                     fs.Duration `config:"scale_up_grace_period"`
		ScaleDownGracePeriod                   fs.Duration `config:"scale_down_grace_period"`
		ScaleDownGracePeriodDuringWorkingHours fs.Duration `config:"scale_down_grace_period_during_working_hours"`
	}
)

// NewMetrics returns a new registered scaler Metrics.
func NewMetrics() *Metrics {
	m := Metrics{
		numScaleUps: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: config.MetricsNamespace,
			Name:      "scale_ups_total",
			Help:      "The total number of attempted scale ups.",
		}, []string{"backend"}),
		numFailedScaleUps: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: config.MetricsNamespace,
			Name:      "scale_ups_failed_total",
			Help:      "The total number of failed scale ups.",
		}, []string{"backend"}),
		numScaleDowns: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: config.MetricsNamespace,
			Name:      "scale_downs_total",
			Help:      "The total number of attempted scale downs.",
		}, []string{"backend"}),
		numFailedScaleDowns: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: config.MetricsNamespace,
			Name:      "scale_downs_failed_total",
			Help:      "The total number of failed scale downs.",
		}, []string{"backend"}),
		numScaleToMinimum: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: config.MetricsNamespace,
			Name:      "scale_to_minimum_total",
			Help:      "The total number of attempted scale to minimum.",
		}, []string{"backend"}),
		numFailedScaleToMinimum: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "jenkins_autoscaler",
			Name:      "scale_to_minimum_failed_total",
			Help:      "The total number of failed scale to minimum.",
		}, []string{"backend"}),
		numGC: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: config.MetricsNamespace,
			Name:      "gc_total",
			Help:      "The total number of attempted gc.",
		}, []string{"backend"}),
		numFailedGC: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: config.MetricsNamespace,
			Name:      "gc_failed_total",
			Help:      "The total number of failed gc.",
		}, []string{"backend"}),
	}

	for _, backend := range backend.Names() {
		m.numScaleUps.WithLabelValues(backend)
		m.numFailedScaleUps.WithLabelValues(backend)
		m.numScaleDowns.WithLabelValues(backend)
		m.numFailedScaleDowns.WithLabelValues(backend)
		m.numScaleToMinimum.WithLabelValues(backend)
		m.numFailedScaleToMinimum.WithLabelValues(backend)
		m.numGC.WithLabelValues(backend)
		m.numFailedGC.WithLabelValues(backend)
	}

	return &m
}

// New returns a new Scaler.
func New(m configmap.Mapper, bk backend.Backend, l *log.Logger, mtr *Metrics) (*Scaler, error) {
	opt := new(Options)
	if err := config.ReadOptions(m, opt); err != nil {
		return nil, err
	}

	clientOpt := new(client.Options)
	if err := config.ReadOptions(m, clientOpt); err != nil {
		return nil, err
	}

	if opt.DryRun {
		l.Info("running in DryRun mode")
	}

	return &Scaler{
		backend:  bk,
		opt:      opt,
		metrics:  mtr,
		client:   client.New(clientOpt),
		schedule: gronx.New(),
		logger: log.NewEntry(l).WithFields(log.Fields{
			"component": "scaler",
			"dryRun":    opt.DryRun,
			"backend":   bk.Name(),
		}),
	}, nil
}

// Do run the auto scaler check
func (s *Scaler) Do(ctx context.Context) {
	s.ctx = ctx

	logger := s.logger

	usage, err := s.client.GetCurrentUsage(ctx)
	if err != nil {
		logger.Errorf("can't get current jenkins usage: %v", err)

		return
	}

	logger.Debugf("current nodes usage is %d%%", usage)

	nodes, err := s.client.GetAllNodes(s.ctx)
	if err != nil {
		logger.Errorf("can't get jenkins nodes: %v", err)

		return
	}

	nodes = nodes.
		ExcludeNode(s.opt.ControllerNodeName).
		ExcludeOffline()

	if nodes.Len() > 0 && usage > s.opt.ScaleUpThreshold {
		logger.Infof("current usage is %d%% > %d%% then specified threshold, will try to scale up", usage, s.opt.ScaleUpThreshold)

		s.metrics.numScaleUps.WithLabelValues(s.backend.Name()).Inc()

		if err := s.scaleUp(usage); err != nil {
			s.metrics.numFailedScaleUps.WithLabelValues(s.backend.Name()).Inc()

			logger.Error(err)
		}

		return
	}

	isMin := s.isMinimumNodes(nodes)
	if isMin && usage < s.opt.ScaleDownThreshold {
		logger.Infof("current usage is %d%% < %d%% then specified threshold, will try to scale down", usage, s.opt.ScaleDownThreshold)

		s.metrics.numScaleDowns.WithLabelValues(s.backend.Name()).Inc()

		if err := s.scaleDown(nodes); err != nil {
			s.metrics.numFailedScaleDowns.WithLabelValues(s.backend.Name()).Inc()

			logger.Error(err)
		}

		return
	}

	logger.Debug("at or under minimum nodes. will adjust to the minimum if needed")

	s.metrics.numScaleToMinimum.WithLabelValues(s.backend.Name()).Inc()

	if err := s.scaleToMinimum(nodes); err != nil {
		s.metrics.numFailedScaleToMinimum.WithLabelValues(s.backend.Name()).Inc()

		logger.Error(err)
	}
}

// scaleUp check if need to scale up more nodes.
func (s *Scaler) scaleUp(usage int64) error {
	logger := s.logger

	if time.Since(s.lastScaleUp) < time.Duration(s.opt.ScaleUpGracePeriod) {
		logger.Info("still in grace period. won't scale up")

		return nil
	}

	curSize, err := s.backend.CurrentSize()
	if err != nil {
		return err
	}

	maxSize := s.opt.MaxNodes
	if curSize >= maxSize {
		logger.Infof("reached maximum of %d nodes. won't scale up", maxSize)

		return nil
	}

	newSize := int64(math.Ceil(float64(curSize) * float64(usage) / float64(s.opt.ScaleUpThreshold)))
	if newSize > maxSize {
		logger.Infof("need %d extra nodes, but can't go over the limit of %d", newSize-curSize, maxSize)

		newSize = maxSize
	}

	if newSize == curSize {
		logger.Debugf("new target size: %d = %d is the same as current, skipping the resize", newSize, curSize)

		return nil
	}

	logger.Infof("will spin up %d extra nodes", newSize-curSize)
	logger.Debugf("new target size: %d", newSize)

	if s.opt.DryRun {
		return nil
	}

	if err := s.backend.Resize(newSize); err != nil {
		return err
	}

	s.lastScaleUp = time.Now()

	return nil
}

// scaleDown check if need to scale down nodes.
func (s *Scaler) scaleDown(nodes client.Nodes) error {
	logger := s.logger
	isWH := s.isWorkingHour()
	lastScaleDown := time.Since(s.lastScaleDown)
	scaleDownWHPeriod := time.Duration(s.opt.ScaleDownGracePeriodDuringWorkingHours)

	if isWH && lastScaleDown < scaleDownWHPeriod {
		logger.Infof("still in grace period during working hours. won't scale down: %v < %v", lastScaleDown, scaleDownWHPeriod)

		return nil
	}

	scaleDownGracePeriod := time.Duration(s.opt.ScaleDownGracePeriod)
	if !isWH && lastScaleDown < scaleDownGracePeriod {
		logger.Infof("still in grace period outside working hours. won't scale down: %v < %v", lastScaleDown, scaleDownGracePeriod)

		return nil
	}

	for _, node := range nodes {
		name := node.GetName()
		if err := s.removeNode(name); err != nil {
			if errors.Is(err, ErrNodeInUse) {
				s.logger.Debugf("node name %s: %v", name, err)

				continue
			}
			// if failing during node destruction, logging error and continue to the next one
			logger.Errorf("failed destroying %s with error %s. continue to next node", name, err.Error())

			continue
		}

		return nil
	}

	logger.Debug("no idle node was found")

	return nil
}

// // scaleToMinimum check if need normalize the num of nodes to minimum count.
func (s *Scaler) scaleToMinimum(nodes client.Nodes) error {
	isWH := s.isWorkingHour()
	minNodes := s.opt.MinNodesInWorkingHours

	if s.opt.DryRun {
		return nil
	}

	if isWH && nodes.Len() < minNodes {
		s.logger.Infof("under minimum of %d nodes during working hours. will adjust to the minimum", minNodes)

		return s.backend.Resize(minNodes)
	}

	if nodes.Len() < 1 {
		s.logger.Info("not a single node off work hours. will adjust to one")

		return s.backend.Resize(1)
	}

	return nil
}

// isMinimumNodes checking if current time is at minimum count.
func (s *Scaler) isMinimumNodes(nodes client.Nodes) bool {
	s.logger.Debugf("number of nodes: %d", nodes.Len())

	isWH := s.isWorkingHour()
	minNodes := s.opt.MinNodesInWorkingHours

	s.logger.Debugf("is working hours now: %v - cron: %s", isWH, s.opt.WorkingHoursCronExpressions)

	if isWH && nodes.Len() > minNodes {
		return true
	}

	if !isWH && nodes.Len() > 1 {
		return true
	}

	return false
}

// isWorkingHour checking if now is working hours.
func (s *Scaler) isWorkingHour() bool {
	if s.opt.DisableWorkingHours {
		return true
	}

	ok, err := s.schedule.IsDue(s.opt.WorkingHoursCronExpressions)
	if err != nil {
		s.logger.Error(err)

		return false
	}

	return ok
}

// removeNode remove the given node name from jenkins and from the cloud.
func (s *Scaler) removeNode(name string) error {
	node, err := s.client.GetNode(s.ctx, name)
	if err != nil {
		return err
	}

	if !node.Raw.Idle {
		return ErrNodeInUse
	}

	if s.opt.DryRun {
		return nil
	}

	ok, err := s.client.DeleteNode(s.ctx, name)
	if err != nil {
		return err
	}

	if !ok {
		return errors.New("can't delete node from jenkins cluster")
	}

	instances, err := s.backend.Instances()
	if err != nil {
		return err
	}

	if ins, ok := instances[name]; ok {
		if err := s.backend.Terminate(backend.NewInstances().Add(ins)); err != nil {
			return err
		}

		s.lastScaleDown = time.Now()

		s.logger.Infof("node %s was removed from cluster", name)

		return nil
	}

	return fmt.Errorf("can't terminate instance %s is missing", name)
}

// GC will look for instance that not in jenkins list of nodes aka (zombie) and will try to remove it.
func (s *Scaler) GC(ctx context.Context) {
	s.metrics.numGC.WithLabelValues(s.backend.Name()).Inc()

	logger := s.logger.WithField("component", "gc")
	if err := s.gc(ctx, logger); err != nil {
		s.metrics.numFailedGC.WithLabelValues(s.backend.Name()).Inc()

		logger.Error(err)
	}
}

func (s *Scaler) gc(ctx context.Context, logger *log.Entry) error {
	logger.Debug("starting GC")

	nodes, err := s.client.GetAllNodes(ctx)
	if err != nil {
		return err
	}

	nodes = nodes.
		ExcludeNode(s.opt.ControllerNodeName)

	instances, err := s.backend.Instances()
	if err != nil {
		return err
	}

	// remove nodes and instances from backend and jenkins master
	var errs error
	ins := backend.NewInstances()
	for _, instance := range instances {
		name := instance.Name()

		// verify that each instance is being seen by Jenkins
		if node, ok := nodes.IsExist(name); ok && !node.Raw.Offline {
			continue
		}

		logger.Infof("found running instance %s which is not registered in Jenkins. will try to remove it", name)

		// lazy load of additional data about instance
		if err = instance.Describe(); err != nil {
			errs = multierror.Append(errs, err)

			continue
		}

		// TODO: let user specify time
		// not taking down nodes that are running less than 20 minutes
		if instanceLunchTime := time.Since(*instance.LaunchTime()); instanceLunchTime < 20*time.Minute {
			logger.Infof("not taking instance %s down since it is running only %v", name, instanceLunchTime)

			continue
		}

		ins.Add(instance)
	}

	if !s.opt.DryRun {
		// get nodes names that not exist in backend
		_, r := lo.Difference(lo.Keys(instances), lo.Keys(nodes))

		// remove zombies nodes from jenkins master that already not exist in backend
		for _, name := range append(r, lo.Keys(ins)...) {
			logger.Infof("removing zombie node %s from Jenkins", name)

			if _, err := s.client.DeleteNode(ctx, name); err != nil {
				errs = multierror.Append(errs, err)
			}
		}

		if ins.Len() > 0 {
			if err = s.backend.Terminate(ins); err != nil {
				errs = multierror.Append(errs, err)
			}
		}
	}

	return errs
}

func (o *Options) Name() string {
	return "scaler"
}
