package gce

import (
	"context"
	"errors"

	"github.com/go-playground/validator/v10"
	"github.com/mitchellh/mapstructure"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"google.golang.org/api/compute/v1"

	"github.com/bringg/jenkins-autoscaler/pkg/backend"
	"github.com/bringg/jenkins-autoscaler/pkg/config"
)

const BackendName = "gce"

type (
	// Options defines the configuration for this backend
	Options struct {
		Project                  string `config:"project" validate:"required"`
		Region                   string `config:"region" validate:"required"`
		InstanceGroupManagerName string `config:"instance_group_manager" validate:"required"`
	}

	Backend struct {
		name string
		ctx  context.Context
		opt  *Options
		srv  *compute.Service
		// use a single instance of Validate, it caches struct info
		validate *validator.Validate
	}
)

func init() {
	backend.Register(&backend.RegInfo{
		Name:       BackendName,
		NewBackend: NewBackend,
		Options: []fs.Option{
			{
				Name: "project",
				Help: "project name",
			},
			{
				Name: "region",
				Help: "region name",
			},
			{
				Name: "instance_group_manager",
				Help: "name of Jenkins nodes Instance Group Manager",
			},
		},
	})
}

// NewBackend constructs an aws Backend
func NewBackend(ctx context.Context, m configmap.Mapper) (backend.Backend, error) {
	// Parse config into Options struct
	opt := new(Options)
	if err := config.ReadOptions(m, opt); err != nil {
		return nil, err
	}

	srv, err := compute.NewService(ctx)
	if err != nil {
		return nil, err
	}

	return &Backend{
		ctx:      ctx,
		opt:      opt,
		srv:      srv,
		validate: validator.New(),
	}, nil
}

func (b *Backend) Name() string {
	return BackendName
}

func (b *Backend) Resize(size int64) error {
	if _, err := b.srv.RegionInstanceGroupManagers.
		Resize(b.opt.Project, b.opt.Region, b.opt.InstanceGroupManagerName, size).
		Do(); err != nil {
		return err
	}

	return nil
}

func (b *Backend) Terminate(instances backend.Instances) error {
	if len(instances) == 0 {
		return errors.New("at least one instance required")
	}

	urls := make([]string, 0)
	for _, v := range instances {
		if i, ok := v.(*instance); ok {
			urls = append(urls, i.raw.Instance)
		}
	}

	rb := &compute.RegionInstanceGroupManagersDeleteInstancesRequest{
		Instances: urls,
	}

	_, err := b.srv.RegionInstanceGroupManagers.DeleteInstances(b.opt.Project, b.opt.Region, b.opt.InstanceGroupManagerName, rb).Do()
	if err != nil {
		return err
	}

	return nil
}

func (b *Backend) CurrentSize() (int64, error) {
	resp, err := b.srv.RegionInstanceGroupManagers.Get(b.opt.Project, b.opt.Region, b.opt.InstanceGroupManagerName).Do()
	if err != nil {
		return 0, err
	}

	return resp.TargetSize, nil
}

func (b *Backend) Instances() (backend.Instances, error) {
	resp, err := b.srv.RegionInstanceGroupManagers.
		ListManagedInstances(b.opt.Project, b.opt.Region, b.opt.InstanceGroupManagerName).
		Context(b.ctx).
		Do()
	if err != nil {
		return nil, err
	}

	instances := make(backend.Instances, len(resp.ManagedInstances))
	for _, v := range resp.ManagedInstances {
		instance, err := b.gceInstanceFromURLInstance(v.Instance)
		if err != nil {
			return nil, err
		}

		instance.raw = v
		instance.srv = b.srv

		instances[instance.IName] = instance
	}

	return instances, nil
}

func (b *Backend) gceInstanceFromURLInstance(url string) (*instance, error) {
	input := ParseURLPath("/compute/v1/projects/{projectID}/zones/{zoneID}/instances/{instanceName}", url)
	output := new(instance)

	if err := mapstructure.Decode(input, output); err != nil {
		return nil, err
	}

	return output, b.validate.Struct(output)
}

func (o *Options) Name() string {
	return BackendName
}
