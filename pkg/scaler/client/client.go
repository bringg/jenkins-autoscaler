package client

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httputil"
	"sync"
	"time"

	"github.com/bndr/gojenkins"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/rclone/rclone/fs"
	"github.com/sirupsen/logrus"
)

type (
	JenkinsAccessor interface {
		GetCurrentUsage(ctx context.Context) (int64, error)
		DeleteNode(ctx context.Context, name string) (bool, error)
		GetAllNodes(ctx context.Context) (Nodes, error)
		GetNode(ctx context.Context, name string) (*gojenkins.Node, error)
	}

	WrapperClient struct {
		*gojenkins.Jenkins

		lastErr time.Time
		opt     *Options
		mu      sync.Mutex
	}

	Nodes map[string]*gojenkins.Node

	Options struct {
		JenkinsURL         string      `config:"jenkins_url" validate:"required"`
		JenkinsUser        string      `config:"jenkins_user" validate:"required"`
		JenkinsToken       string      `config:"jenkins_token" validate:"required"`
		ControllerNodeName string      `config:"controller_node_name"`
		NodeNumExecutors   int64       `config:"node_num_executors"`
		ErrGracePeriod     fs.Duration `config:"err_grace_period"`
	}
)

// New returns a new Client.
func New(opt *Options) *WrapperClient {
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 2
	retryClient.Logger = &logrus.Logger{Out: io.Discard}

	return &WrapperClient{
		opt: opt,
		Jenkins: gojenkins.CreateJenkins(
			retryClient.StandardClient(),
			opt.JenkinsURL,
			opt.JenkinsUser,
			opt.JenkinsToken,
		),
	}
}

// GetCurrentUsage return the current usage of jenkins nodes.
func (c *WrapperClient) GetCurrentUsage(ctx context.Context) (int64, error) {
	computers, err := c.computers(ctx)
	if err != nil {
		return 0, err
	}

	nodes := c.getNodes(computers).
		ExcludeNode(c.opt.ControllerNodeName).
		ExcludeOffline()

	currentUsage := (float64(computers.BusyExecutors) / float64(nodes.Len()*c.opt.NodeNumExecutors)) * 100

	if math.IsNaN(currentUsage) || math.IsInf(currentUsage, 0) {
		return 0, nil
	}

	return int64(currentUsage), nil
}

func (c *WrapperClient) getNodes(computers *gojenkins.Computers) Nodes {
	nodes := make(Nodes, len(computers.Computers))
	for _, node := range computers.Computers {
		nodes[node.DisplayName] = &gojenkins.Node{Jenkins: c.Jenkins, Raw: node, Base: "/computer/" + node.DisplayName}
	}

	return nodes
}

func (c *WrapperClient) GetAllNodes(ctx context.Context) (Nodes, error) {
	computers, err := c.computers(ctx)
	if err != nil {
		return nil, err
	}

	return c.getNodes(computers), nil
}

func (n Nodes) Len() int64 {
	return int64(len(n))
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
	lastErr := time.Since(c.lastErr)
	lastErrPeriod := time.Duration(c.opt.ErrGracePeriod)

	if lastErr < lastErrPeriod {
		return nil, fmt.Errorf("still in error grace period. skipping request: %v < %v", lastErr, lastErrPeriod)
	}

	computers, err := func() (*gojenkins.Computers, error) {
		computers := new(gojenkins.Computers)

		qr := map[string]string{
			"depth": "1",
		}

		res, err := c.Requester.GetJSON(ctx, "/computer", computers, qr)
		if err != nil {
			return nil, err
		}

		if res.StatusCode != http.StatusOK {
			body, _ := httputil.DumpResponse(res, true)

			return nil, fmt.Errorf("api response status code %d, body dump: %q", res.StatusCode, body)
		}

		return computers, nil
	}()

	if err != nil {
		c.mu.Lock()
		defer c.mu.Unlock()

		c.lastErr = time.Now()

		return nil, err
	}

	return computers, nil
}

func (o *Options) Name() string {
	return "jenkins client"
}
