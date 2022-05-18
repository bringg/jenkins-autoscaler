package gce

import (
	"time"

	"google.golang.org/api/compute/v1"
)

type instance struct {
	raw *compute.ManagedInstance
	srv *compute.Service

	launchTime *time.Time
	Project    string `mapstructure:"projectID" validate:"required"`
	IName      string `mapstructure:"instanceName" validate:"required"`
	Zone       string `mapstructure:"zoneID" validate:"required"`
}

func (i instance) Name() string {
	return i.IName
}

func (i instance) LaunchTime() *time.Time {
	return i.launchTime
}

func (i *instance) Describe() error {
	ins, err := i.srv.Instances.Get(i.Project, i.Zone, i.IName).Do()
	if nil != err {
		return err
	}

	launchTime, err := time.Parse(time.RFC3339, ins.CreationTimestamp)
	if err != nil {
		return err
	}

	i.launchTime = &launchTime

	return nil
}
