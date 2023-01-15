package scaler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
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
		WorkingHoursCronExpressions            string      `config:"working_hours_cron_expressions"`
		ControllerNodeName                     string      `config:"controller_node_name"`
		NodeWithLabel                          string      `config:"nodes_with_label"`
		MaxNodes                               int64       `config:"max_nodes"`
		MinNodesInWorkingHours                 int64       `config:"min_nodes_during_working_hours"`
		ScaleUpThreshold                       int64       `config:"scale_up_threshold"`
		ScaleDownThreshold                     int64       `config:"scale_down_threshold"`
		NodeNumExecutors                       int64       `config:"node_num_executors"`
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

	// get backend instances to calculate usage
	ins, err := s.backend.Instances()
	if err != nil {
		logger.Errorf("can't get backend instances: %v", err)

		return
	}

	nodes, err := s.client.GetAllNodes(s.ctx)
	if err != nil {
		logger.Errorf("can't get jenkins nodes: %v", err)

		return
	}

	nodes = nodes.
		ExcludeNode(s.opt.ControllerNodeName).
		ExcludeOffline().
		KeepWithLabel(s.opt.NodeWithLabel)

	usage := s.getCurrentUsage(nodes)

	logger.Debugf("current utilization of nodes is %d%%", usage)

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

		if err := s.scaleDown(nodes, ins); err != nil {
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

	if s.isScaleUpGracePeriod() {
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
func (s *Scaler) scaleDown(nodes client.Nodes, instances backend.Instances) error {
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
		if err := s.removeNode(name, instances); err != nil {
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

// isScaleUpGracePeriod check if is still in scale up grace period
func (s *Scaler) isScaleUpGracePeriod() bool {
	lastScaleUp := time.Since(s.lastScaleUp)
	scaleUpGracePeriod := time.Duration(s.opt.ScaleUpGracePeriod)

	if lastScaleUp < scaleUpGracePeriod {
		s.logger.Infof("still in grace period. won't scale up: %v < %v", lastScaleUp, scaleUpGracePeriod)

		return true
	}

	return false
}

// scaleToMinimum check if need normalize the num of nodes to minimum count.
func (s *Scaler) scaleToMinimum(nodes client.Nodes) error {
	isWH := s.isWorkingHour()
	minNodes := s.opt.MinNodesInWorkingHours

	if s.isScaleUpGracePeriod() {
		return nil
	}

	if s.opt.DryRun {
		return nil
	}

	if isWH && nodes.Len() < minNodes {
		s.logger.Infof("under minimum of %d nodes during working hours. will adjust to the minimum", minNodes)

		if err := s.backend.Resize(minNodes); err != nil {
			return err
		}

		s.lastScaleUp = time.Now()

		return nil
	}

	if nodes.Len() < 1 {
		s.logger.Info("not a single node off work hours. will adjust to one")

		if err := s.backend.Resize(1); err != nil {
			return err
		}

		s.lastScaleUp = time.Now()
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
func (s *Scaler) removeNode(name string, instances backend.Instances) error {
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
		return errors.New("can't delete node from jenkins")
	}

	if ins, ok := instances[name]; ok {
		if err := s.backend.Terminate(backend.NewInstances().Add(ins)); err != nil {
			return err
		}

		s.lastScaleDown = time.Now()

		s.logger.Infof("instance %s was removed from the backend", name)

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
		ExcludeNode(s.opt.ControllerNodeName).
		KeepWithLabel(s.opt.NodeWithLabel)

	instances, err := s.backend.Instances()
	if err != nil {
		return err
	}

	insToDel := s.findInstancesToDelete(instances, nodes, logger)
	insToDelKeys := lo.Keys(insToDel)

	// instance not exist, but jenkins node registered in offline
	_, nodesToDel := lo.Difference(lo.Keys(instances), lo.Keys(nodes))

	// delete nodes from jenkins
	var errs error
	for _, name := range append(nodesToDel, insToDelKeys...) {
		logger.Infof("removing node %s from Jenkins", name)

		if s.opt.DryRun {
			continue
		}

		if _, err := s.client.DeleteNode(ctx, name); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	// delete instance from cloud backend
	if insToDel.Len() > 0 {
		logger.Infof("found %d running instances (%s) which are not registered in Jenkins. Will try to remove them", insToDel.Len(), strings.Join(insToDelKeys, ","))

		if s.opt.DryRun {
			return errs
		}

		if err = s.backend.Terminate(insToDel); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	return errs
}

// getCurrentUsage return the current usage of jenkins nodes.
func (s *Scaler) getCurrentUsage(nodes client.Nodes) int64 {
	busyExecutors := 0
	for _, node := range nodes {
		for _, executor := range node.Raw.Executors {
			if !executor.Idle {
				busyExecutors++
			}
		}
	}

	currentUsage := (float64(busyExecutors) / float64(nodes.Len()*s.opt.NodeNumExecutors)) * 100

	if math.IsNaN(currentUsage) || math.IsInf(currentUsage, 0) {
		return 0
	}

	return int64(currentUsage)
}

// findInstancesToDelete get instances that missing in jenkins and lunchTime is more then specified duration.
func (s *Scaler) findInstancesToDelete(instances backend.Instances, nodes client.Nodes, logger *log.Entry) backend.Instances {
	insToDel := backend.NewInstances()
	for _, instance := range instances {
		// verify that each instance is being seen by Jenkins
		name := instance.Name()
		if node, ok := nodes[name]; ok && !node.Raw.Offline {
			continue
		}

		// lazy load of additional data about instance
		if err := instance.Describe(); err != nil {
			logger.Error(err)

			continue
		}

		// TODO: let user specify time
		// not taking down nodes that are running less than 20 minutes
		if instanceLunchTime := time.Since(*instance.LaunchTime()); instanceLunchTime < 20*time.Minute {
			logger.Infof("skipping instance %s since it is running only %v", name, instanceLunchTime)

			continue
		}

		insToDel.Add(instance)
	}

	return insToDel
}

func (o *Options) Name() string {
	return "scaler"
}
