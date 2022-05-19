package scaler_test

import (
	"context"
	"time"

	"bou.ke/monkey"
	"github.com/bndr/gojenkins"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"

	"github.com/bringg/jenkins-autoscaler/pkg/backend"
	"github.com/bringg/jenkins-autoscaler/pkg/scaler"
	mock_backend "github.com/bringg/jenkins-autoscaler/pkg/testing/mocks/backend"
	mock_client "github.com/bringg/jenkins-autoscaler/pkg/testing/mocks/scaler"
)

var _ = Describe("Scaler", func() {
	var scal *scaler.Scaler
	var ctx context.Context
	var err error
	var client *mock_client.MockJenkinser
	var bk *mock_backend.MockBackend
	var patch *monkey.PatchGuard
	var logger *logrus.Logger
	var cfg configmap.Simple

	var mockController *gomock.Controller

	metrics := scaler.NewMetrics()

	BeforeEach(func() {
		wayback := time.Date(2022, time.February, 27, 6, 2, 3, 4, time.UTC)
		patch = monkey.Patch(time.Now, func() time.Time { return wayback })
		mockController, ctx = gomock.WithContext(context.Background(), GinkgoT())

		client = mock_client.NewMockJenkinser(mockController)
		bk = mock_backend.NewMockBackend(mockController)
		bk.EXPECT().Name().MinTimes(1)

		cfg = make(configmap.Simple)
		cfg.Set("jenkins_url", "http://foo.poo")
		cfg.Set("jenkins_user", "foo")
		cfg.Set("jenkins_token", "poo")
		cfg.Set("run_interval", "1s")
		cfg.Set("gc_run_interval", "1s")
		cfg.Set("scale_up_threshold", "70")
		cfg.Set("scale_up_grace_period", "5m")
		cfg.Set("scale_down_grace_period", "10m")
		cfg.Set("scale_down_threshold", "30")
		cfg.Set("max_nodes", "10")
		cfg.Set("min_nodes_during_working_hours", "2")
		cfg.Set("scale_down_grace_period_during_working_hours", "1h")
		cfg.Set("disable_working_hours", "true")

		logger, _ = test.NewNullLogger()
		scal, err = scaler.NewWithClient(cfg, bk, client, logger, metrics)
		Expect(err).To(Not(HaveOccurred()))
	})

	AfterEach(func() {
		patch.Unpatch()
		mockController.Finish()
	})

	Describe("GC", func() {
		It("clear zombies", func() {
			client.EXPECT().GetAllNodes(gomock.Any()).Return(makeFakeNodes(3), nil).Times(1)
			// provider will decrease instances to 3
			bk.EXPECT().Instances().Return(makeFakeInstances(5), nil).Times(1)
			bk.EXPECT().Terminate(gomock.Any()).DoAndReturn(func(ins backend.Instances) error {
				Expect(ins).To(HaveLen(2))

				return nil
			}).Times(1)

			scal.GC(ctx)
		})

		It("clear also if node is exist but in offline", func() {
			nodes := mergeFakeTypes(makeFakeNodes(2, WithOffline()), makeFakeNodes(4))

			// this function should already check if jenkins node is offline and not include it in result but we have another check for sure
			client.EXPECT().GetAllNodes(gomock.Any()).Return(nodes, nil).Times(1)
			bk.EXPECT().Instances().Return(makeFakeInstances(6), nil).Times(1)
			bk.EXPECT().Terminate(gomock.Any()).DoAndReturn(func(ins backend.Instances) error {
				Expect(ins).To(HaveLen(2))
				return nil
			}).Times(1)

			scal.GC(ctx)
		})
	})

	Describe("Do", func() {
		Context("scaleUp", func() {
			It("run provider with min nodes and havy usage, will scale out provider to 1 more", func() {
				// 77% usage, and default scale up threshold is 70%
				client.EXPECT().GetCurrentUsage(gomock.Any(), gomock.Any()).Return(int64(77), nil).Times(1)
				client.EXPECT().GetAllNodes(gomock.Any()).Return(makeFakeNodes(3), nil).Times(1)
				// cloud backend have 2 running instances, this is default min value
				bk.EXPECT().CurrentSize().Return(int64(3), nil).Times(1)
				// provider will use new value of 3
				bk.EXPECT().Resize(int64(4)).Return(nil).Times(1)

				scal.Do(ctx)
			})
		})
		Context("scaleDown", func() {
			It("run provider with 5 nodes and low usage, will scale in provider, decrease from 5 to 4", func() {
				// 28% usage, and default scale down threshold is 30%
				client.EXPECT().GetCurrentUsage(gomock.Any(), gomock.Any()).Return(int64(28), nil).Times(1)
				// will retrun current provider running nodes
				nodes := makeFakeNodes(5)
				client.EXPECT().GetAllNodes(gomock.Any()).Return(nodes, nil).Times(1)
				client.EXPECT().GetNode(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, name string) (*gojenkins.Node, error) {
					return nodes[name], nil
				}).Times(1)
				client.EXPECT().DeleteNode(gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
				// provider will decrease instances to 4
				bk.EXPECT().Instances().Return(makeFakeInstances(5), nil).Times(1)
				bk.EXPECT().Terminate(gomock.Any()).Return(nil).Times(1)

				scal.Do(ctx)
			})
		})
		Context("scaleToMinimum", func() {
			It("run provider with 0 nodes, will scale out provider to default values", func() {
				// no usage, cluster is empty of jobs, starting the day
				client.EXPECT().GetCurrentUsage(gomock.Any(), gomock.Any()).Return(int64(98), nil).Times(1)
				// no running nodes
				client.EXPECT().GetAllNodes(gomock.Any()).Return(make(scaler.Nodes), nil).Times(1)
				// provider will use default value of 2
				bk.EXPECT().Resize(int64(2)).Return(nil).Times(1)

				scal.Do(ctx)
			})

			It("run provider with 0 nodes, not in working hours", func() {
				cfg.Set("disable_working_hours", "false")

				scal, err = scaler.NewWithClient(cfg, bk, client, logger, metrics)
				Expect(err).To(Not(HaveOccurred()))

				// no usage, cluster is empty of jobs, starting the day
				client.EXPECT().GetCurrentUsage(gomock.Any(), gomock.Any()).Return(int64(0), nil).Times(1)
				// no running nodes
				client.EXPECT().GetAllNodes(gomock.Any()).Return(make(scaler.Nodes), nil).Times(1)
				// provider will use default value of 1
				bk.EXPECT().Resize(int64(1)).Return(nil).Times(1)

				scal.Do(ctx)
			})

			It("run provider with 0 nodes, in working hours", func() {
				scal, err = scaler.NewWithClient(cfg, bk, client, logger, metrics)
				Expect(err).To(Not(HaveOccurred()))

				// no usage, cluster is empty of jobs, starting the day
				client.EXPECT().GetCurrentUsage(gomock.Any(), gomock.Any()).Return(int64(0), nil).Times(1)
				// no running nodes
				client.EXPECT().GetAllNodes(gomock.Any()).Return(make(scaler.Nodes), nil).Times(1)
				// provider will use default value of 2
				bk.EXPECT().Resize(int64(2)).Return(nil).Times(1)

				scal.Do(ctx)
			})
		})
	})
})
