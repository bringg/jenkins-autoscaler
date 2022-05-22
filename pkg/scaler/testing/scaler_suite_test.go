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

	NodeOption func(c *gojenkins.Node)
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

func mergeFakeTypes[M ~map[K]V, K string, V any](dst, src M) M {
	i := len(dst) - 1
	for _, v := range src {
		i++
		dst[K(fmt.Sprintf("%d", i))] = v
	}

	return dst
}

func WithOffline() NodeOption {
	return func(n *gojenkins.Node) {
		n.Raw.Offline = true
	}
}

func makeFakeNodes(size int, opts ...NodeOption) scaler.Nodes {
	nodes := make(scaler.Nodes, size)
	for i := 0; i < size; i++ {
		name := fmt.Sprintf("%d", i)

		nodes[name] = &gojenkins.Node{
			Raw: &gojenkins.NodeResponse{
				DisplayName: name,
				Idle:        true,
			},
		}

		for _, opt := range opts {
			opt(nodes[name])
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
