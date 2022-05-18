package all

import (
	// add all providers
	_ "github.com/bringg/jenkins-autoscaler/pkg/backend/type/aws"
	_ "github.com/bringg/jenkins-autoscaler/pkg/backend/type/gce"
)
