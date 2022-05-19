package scaler

import (
	"context"

	"github.com/bndr/gojenkins"
)

type (
	WrapperClient struct {
		*gojenkins.Jenkins

		opt *Options
	}

	Nodes map[string]*gojenkins.Node
)

// NewClient returns a new Client.
func NewClient(opt *Options) *WrapperClient {
	return &WrapperClient{
		opt: opt,
		Jenkins: gojenkins.CreateJenkins(
			nil,
			opt.JenkinsURL,
			opt.JenkinsUser,
			opt.JenkinsToken,
		),
	}
}

// GetCurrentUsage return the current usage of jenkins nodes.
func (c *WrapperClient) GetCurrentUsage(ctx context.Context, numNodes int64) (int64, error) {
	computers, err := c.computers(ctx)
	if err != nil {
		return 0, err
	}

	currentUsage := int64((float64(computers.BusyExecutors) / float64(numNodes*c.opt.NodeNumExecutors)) * 100)

	return currentUsage, nil
}

func (c *WrapperClient) GetAllNodes(ctx context.Context) (Nodes, error) {
	computers, err := c.computers(ctx)
	if err != nil {
		return nil, err
	}

	nodes := make(Nodes, len(computers.Computers))
	for _, node := range computers.Computers {
		nodes[node.DisplayName] = &gojenkins.Node{Jenkins: c.Jenkins, Raw: node, Base: "/computer/" + node.DisplayName}
	}

	return nodes, nil
}

func (n Nodes) IsExist(name string) (*gojenkins.Node, bool) {
	if node, ok := n[name]; ok {
		return node, true
	}

	return nil, false
}

func (n Nodes) ExcludeOffline() Nodes {
	nodes := make(Nodes, 0)
	for name, node := range n {
		if node.Raw.Offline == true {
			continue
		}

		nodes[name] = node
	}

	return nodes
}

func (n Nodes) ExcludeNode(name string) Nodes {
	nodes := make(Nodes, 0)
	for i, node := range n {
		if name == node.Raw.DisplayName {
			continue
		}

		nodes[i] = node
	}

	return nodes
}

func (c *WrapperClient) computers(ctx context.Context) (*gojenkins.Computers, error) {
	computers := new(gojenkins.Computers)

	qr := map[string]string{
		"depth": "1",
	}

	_, err := c.Requester.GetJSON(ctx, "/computer", computers, qr)
	if err != nil {
		return nil, err
	}

	return computers, nil
}
