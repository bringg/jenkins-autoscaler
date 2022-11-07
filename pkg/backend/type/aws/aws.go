package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"

	"github.com/bringg/jenkins-autoscaler/pkg/backend"
	"github.com/bringg/jenkins-autoscaler/pkg/config"
)

const BackendName = "aws"

type (
	// Options defines the configuration for this backend
	Options struct {
		AutoScalingGroupName string `config:"autoscaling_group_name" validate:"required"`
	}

	Backend struct {
		ctx       context.Context // global context for reading config
		opt       *Options
		asClient  *autoscaling.Client
		ec2Client *ec2.Client
	}
)

func init() {
	backend.Register(&backend.RegInfo{
		Name:       BackendName,
		NewBackend: NewBackend,
		Options: []fs.Option{
			{
				Name: "autoscaling_group_name",
				Help: "jenkins agent nodes autoscaling group name",
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

	cfg, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "configuration error")
	}

	return &Backend{
		ctx:       ctx,
		opt:       opt,
		asClient:  autoscaling.NewFromConfig(cfg.Copy()),
		ec2Client: ec2.NewFromConfig(cfg.Copy()),
	}, nil
}

func (b *Backend) Name() string {
	return BackendName
}

func (b *Backend) Resize(size int64) error {
	if _, err := b.asClient.SetDesiredCapacity(b.ctx, &autoscaling.SetDesiredCapacityInput{
		AutoScalingGroupName: aws.String(b.opt.AutoScalingGroupName),
		DesiredCapacity:      aws.Int32(int32(size)),
	}); err != nil {
		return err
	}

	return nil
}

func (b *Backend) Terminate(instances backend.Instances) error {
	if len(instances) == 0 {
		return errors.New("at least one instance required")
	}

	var err error
	for _, i := range instances {
		if v, ok := i.(*instance); ok {
			if _, e := b.asClient.TerminateInstanceInAutoScalingGroup(b.ctx, &autoscaling.TerminateInstanceInAutoScalingGroupInput{
				InstanceId:                     v.raw.InstanceId,
				ShouldDecrementDesiredCapacity: aws.Bool(true),
			}); e != nil {
				err = multierror.Append(err, e)

				continue
			}
		}
	}

	return err
}

func (b *Backend) autoScalingGroup() (*types.AutoScalingGroup, error) {
	out, err := b.asClient.DescribeAutoScalingGroups(b.ctx, &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{b.opt.AutoScalingGroupName},
		MaxRecords:            aws.Int32(1),
	})
	if err != nil {
		return nil, err
	}

	// if group is not found or there is typo in name aws api not throwing any error,
	// so need to check the len of groups.
	if len(out.AutoScalingGroups) == 0 {
		return nil, fmt.Errorf("autoscaling group %q not found", b.opt.AutoScalingGroupName)
	}

	return &out.AutoScalingGroups[0], nil
}

func (b *Backend) Instances() (backend.Instances, error) {
	group, err := b.autoScalingGroup()
	if err != nil {
		return nil, err
	}

	instanceIds := make([]string, len(group.Instances))
	for i, instance := range group.Instances {
		instanceIds[i] = *instance.InstanceId
	}

	// if we gonna pass empty ids to api it will return any instances not related to group...
	instances := make(backend.Instances, len(instanceIds))
	if len(instanceIds) == 0 {
		return instances, nil
	}

	result, err := b.ec2Client.DescribeInstances(b.ctx, &ec2.DescribeInstancesInput{
		InstanceIds: instanceIds,
	})
	if err != nil {
		return nil, err
	}

	for _, r := range result.Reservations {
		for _, i := range r.Instances {
			instances[*i.PrivateDnsName] = &instance{
				raw:        i,
				name:       *i.PrivateDnsName,
				launchTime: i.LaunchTime,
			}
		}
	}

	return instances, nil
}

func (b *Backend) CurrentSize() (int64, error) {
	group, err := b.autoScalingGroup()
	if err != nil {
		return 0, err
	}

	return int64(*group.DesiredCapacity), nil
}

func (o *Options) Name() string {
	return BackendName
}
