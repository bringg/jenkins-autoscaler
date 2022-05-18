package scaler_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/bndr/gojenkins"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bringg/jenkins-autoscaler/pkg/backend"
	"github.com/bringg/jenkins-autoscaler/pkg/scaler"
)

type (
	fakeInstance struct {
		name       string
		launchTime *time.Time
	}
)

func (i fakeInstance) Describe() error {
	return nil
}

func (i fakeInstance) Name() string {
	return i.name
}

func (i fakeInstance) LaunchTime() *time.Time {
	return i.launchTime
}

func makeFakeNodes(size int) scaler.Nodes {
	nodes := make(scaler.Nodes, size)
	for i := 0; i < size; i++ {
		name := fmt.Sprintf("%d", i)
		nodes[name] = &gojenkins.Node{
			Raw: &gojenkins.NodeResponse{
				DisplayName: name,
				Idle:        true,
			},
		}
	}

	return nodes
}

func makeFakeInstances(size int) backend.Instances {
	ins := backend.NewInstances()
	for i := 0; i < size; i++ {
		name := fmt.Sprintf("%d", i)
		now := time.Now().Add(-30 * time.Minute)
		ins.Add(&fakeInstance{name: name, launchTime: &now})
	}

	return ins
}

func TestProvider(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Provider Suite")
}
