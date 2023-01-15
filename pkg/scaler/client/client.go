package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/bndr/gojenkins"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

type (
	JenkinsAccessor interface {
		DeleteNode(ctx context.Context, name string) (bool, error)
		GetAllNodes(ctx context.Context) (Nodes, error)
		GetNode(ctx context.Context, name string) (*gojenkins.Node, error)
	}

	WrapperClient struct {
		*gojenkins.Jenkins

		lastErr time.Time
		opt     *Options
	}

	Nodes map[string]*gojenkins.Node

	Options struct {
		JenkinsURL     string        `config:"jenkins_url" validate:"required"`
		JenkinsUser    string        `config:"jenkins_user" validate:"required"`
		JenkinsToken   string        `config:"jenkins_token" validate:"required"`
		LastErrBackoff time.Duration `config:"last_err_backoff"`
	}
)

// New returns a new Client.
func New(opt *Options) *WrapperClient {
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 2
	retryClient.Logger = &logrus.Logger{Out: io.Discard}

	opt.LastErrBackoff = 2 * time.Minute

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
		if node.Raw.Offline {
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

func (n Nodes) KeepWithLabel(label string) Nodes {
	if label == "" {
		return n
	}

	return lo.PickBy(n, func(name string, node *gojenkins.Node) bool {
		return lo.ContainsBy(node.Raw.AssignedLabels, func(item map[string]string) bool {
			v, ok := item["name"]
			return ok && strings.Compare(v, label) == 0
		})
	})
}

func (c *WrapperClient) computers(ctx context.Context) (*gojenkins.Computers, error) {
	lastErr := time.Since(c.lastErr)
	lastErrBackoff := c.opt.LastErrBackoff

	if lastErr < lastErrBackoff {
		return nil, fmt.Errorf("request rejected because jenkins API was in-accessible: %v < %v", lastErr, lastErrBackoff)
	}

	computers, err := func() (*gojenkins.Computers, error) {
		computers := new(gojenkins.Computers)

		qr := map[string]string{
			"depth": "2",
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
		c.lastErr = time.Now()

		return nil, err
	}

	return computers, nil
}

func (o *Options) Name() string {
	return "jenkins client"
}
