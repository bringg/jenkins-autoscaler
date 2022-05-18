package aws

import (
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type instance struct {
	raw types.Instance

	name       string
	launchTime *time.Time
}

func (i instance) Name() string {
	return i.name
}

func (i instance) LaunchTime() *time.Time {
	return i.launchTime
}

func (i *instance) Describe() error {
	return nil
}
