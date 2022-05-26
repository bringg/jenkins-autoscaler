package scaler

import (
	"context"
	"time"

	"github.com/adhocore/gronx"
	"github.com/bndr/gojenkins"
	"github.com/golang/mock/gomock"
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"

	"github.com/bringg/jenkins-autoscaler/pkg/backend"
	jclient "github.com/bringg/jenkins-autoscaler/pkg/scaler/client"
	mock_backend "github.com/bringg/jenkins-autoscaler/pkg/testing/mocks/backend"
	mock_client "github.com/bringg/jenkins-autoscaler/pkg/testing/mocks/scaler"
)

var _ = g.Describe("Scaler", func() {
	g.Describe("Public Functions", func() {
		var scal *Scaler
		var ctx context.Context
		var err error
		var client *mock_client.MockJenkinser
		var bk *mock_backend.MockBackend
		var logger *logrus.Logger
		var cfg configmap.Simple

		var mockController *gomock.Controller

		metrics := NewMetrics()

		g.BeforeEach(func() {
			mockController, ctx = gomock.WithContext(context.Background(), g.GinkgoT())

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
			cfg.Set("scale_up_grace_period", "1s")
			cfg.Set("scale_down_grace_period", "10m")
			cfg.Set("scale_down_threshold", "30")
			cfg.Set("max_nodes", "10")
			cfg.Set("min_nodes_during_working_hours", "2")
			cfg.Set("scale_down_grace_period_during_working_hours", "1h")
			cfg.Set("working_hours_cron_expressions", "* * * * *")

			logger, _ = test.NewNullLogger()
			logger.SetOutput(g.GinkgoWriter)
			logger.SetLevel(logrus.DebugLevel)

			scal, err = NewWithClient(cfg, bk, client, logger, metrics)
			o.Expect(err).To(o.Not(o.HaveOccurred()))
		})

		g.AfterEach(func() {
			mockController.Finish()
		})

		g.Describe("GC", func() {
			g.It("clear zombies", func() {
				client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(3), nil).Times(1)
				// provider will decrease instances to 3
				bk.EXPECT().Instances().Return(MakeFakeInstances(5), nil).Times(1)
				bk.EXPECT().Terminate(gomock.Any()).DoAndReturn(func(ins backend.Instances) error {
					o.Expect(ins).To(o.HaveLen(2))

					return nil
				}).Times(1)

				client.EXPECT().DeleteNode(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)

				scal.GC(ctx)
			})

			g.It("clear also if node is exist but in offline", func() {
				nodes := MergeFakeTypes(MakeFakeNodes(2, WithOffline()), MakeFakeNodes(4))

				// this function should already check if jenkins node is offline and not include it in result but we have another check for sure
				client.EXPECT().GetAllNodes(gomock.Any()).Return(nodes, nil).Times(1)
				bk.EXPECT().Instances().Return(MakeFakeInstances(6), nil).Times(1)
				bk.EXPECT().Terminate(gomock.Any()).DoAndReturn(func(ins backend.Instances) error {
					o.Expect(ins).To(o.HaveLen(2))

					return nil
				}).Times(1)

				client.EXPECT().DeleteNode(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)

				scal.GC(ctx)
			})
		})

		g.Describe("Do", func() {
			g.Context("scaleUp", func() {
				g.It("run provider with 1 node, not in working hours, with usage over threshold", func() {
					cfg.Set("working_hours_cron_expressions", "@yearly")

					scal, err = NewWithClient(cfg, bk, client, logger, metrics)
					o.Expect(err).To(o.Not(o.HaveOccurred()))

					// 80% usage, and default scale up threshold is 70%
					client.EXPECT().GetCurrentUsage(gomock.Any()).Return(int64(80), nil).Times(1)
					// no running nodes
					client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(1), nil).Times(1)

					// cloud backend have 1 running instances, this is default min value
					bk.EXPECT().CurrentSize().Return(int64(1), nil).Times(1)
					// provider will use new value of 2
					bk.EXPECT().Resize(int64(2)).Return(nil).Times(1)

					scal.Do(ctx)
				})

				g.It("run provider with min nodes and havy usage, will scale out provider to 1 more", func() {
					// 77% usage, and default scale up threshold is 70%
					client.EXPECT().GetCurrentUsage(gomock.Any()).Return(int64(77), nil).Times(1)
					client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(1), nil).Times(1)
					// cloud backend have 1 running instances, this is default min value
					bk.EXPECT().CurrentSize().Return(int64(1), nil).Times(1)
					// provider will use new value of 2
					bk.EXPECT().Resize(int64(2)).Return(nil).Times(1)

					scal.Do(ctx)

					time.Sleep(time.Second)

					// 77% usage, and default scale up threshold is 70%
					client.EXPECT().GetCurrentUsage(gomock.Any()).Return(int64(77), nil).Times(1)
					client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(2), nil).Times(1)
					// cloud backend have 2 running instances, this is default min value
					bk.EXPECT().CurrentSize().Return(int64(2), nil).Times(1)
					// provider will use new value of 3
					bk.EXPECT().Resize(int64(3)).Return(nil).Times(1)

					scal.Do(ctx)

					time.Sleep(time.Second)

					// 100% usage, and default scale up threshold is 70%
					client.EXPECT().GetCurrentUsage(gomock.Any()).Return(int64(100), nil).Times(1)
					client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(5), nil).Times(1)
					// cloud backend have 5 running instances, this is default min value
					bk.EXPECT().CurrentSize().Return(int64(5), nil).Times(1)
					// provider will use new value of 8
					bk.EXPECT().Resize(int64(8)).Return(nil).Times(1)

					scal.Do(ctx)
				})
			})
			g.Context("scaleDown", func() {
				g.It("run provider with 5 nodes and low usage, will scale in provider, decrease from 5 to 4", func() {
					// 28% usage, and default scale down threshold is 30%
					client.EXPECT().GetCurrentUsage(gomock.Any()).Return(int64(28), nil).Times(1)
					// will retrun current provider running nodes
					nodes := MakeFakeNodes(5)
					client.EXPECT().GetAllNodes(gomock.Any()).Return(nodes, nil).Times(1)
					client.EXPECT().GetNode(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, name string) (*gojenkins.Node, error) {
						return nodes[name], nil
					}).Times(1)
					client.EXPECT().DeleteNode(gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
					// provider will decrease instances to 4
					bk.EXPECT().Instances().Return(MakeFakeInstances(5), nil).Times(1)
					bk.EXPECT().Terminate(gomock.Any()).Return(nil).Times(1)

					scal.Do(ctx)

					// will show "still in grace period during working hours"

					// 28% usage, and default scale down threshold is 30%
					client.EXPECT().GetCurrentUsage(gomock.Any()).Return(int64(28), nil).Times(1)
					// will retrun current provider running nodes
					client.EXPECT().GetAllNodes(gomock.Any()).Return(nodes, nil).Times(1)

					scal.Do(ctx)
				})
			})
			g.Context("scaleToMinimum", func() {
				g.It("run provider with 1 node, in working hours, with usage under threshold", func() {
					// 45% usage and default scale up threshold is 70%
					client.EXPECT().GetCurrentUsage(gomock.Any()).Return(int64(45), nil).Times(1)
					// no running nodes
					client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(1), nil).Times(1)
					// provider will use default value of 1
					bk.EXPECT().Resize(int64(2)).Return(nil).Times(1)

					scal.Do(ctx)
				})
				g.It("run provider with 1 node, not in working hours, with usage under threshold", func() {
					cfg.Set("working_hours_cron_expressions", "@yearly")

					scal, err = NewWithClient(cfg, bk, client, logger, metrics)
					o.Expect(err).To(o.Not(o.HaveOccurred()))

					// 45% usage and default scale up threshold is 70%
					client.EXPECT().GetCurrentUsage(gomock.Any()).Return(int64(45), nil).Times(1)
					// no running nodes
					client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(1), nil).Times(1)

					scal.Do(ctx)
				})

				g.It("run provider with 0 nodes, not in working hours", func() {
					cfg.Set("working_hours_cron_expressions", "@yearly")

					scal, err = NewWithClient(cfg, bk, client, logger, metrics)
					o.Expect(err).To(o.Not(o.HaveOccurred()))

					// no usage, cluster is empty of jobs, starting the day
					client.EXPECT().GetCurrentUsage(gomock.Any()).Return(int64(0), nil).Times(1)
					// no running nodes
					client.EXPECT().GetAllNodes(gomock.Any()).Return(make(jclient.Nodes), nil).Times(1)
					// provider will use default value of 1
					bk.EXPECT().Resize(int64(1)).Return(nil).Times(1)

					scal.Do(ctx)
				})

				g.It("run provider with 0 nodes, in working hours", func() {
					// no usage, cluster is empty of jobs, starting the day
					client.EXPECT().GetCurrentUsage(gomock.Any()).Return(int64(0), nil).Times(1)
					// no running nodes
					client.EXPECT().GetAllNodes(gomock.Any()).Return(make(jclient.Nodes), nil).Times(1)
					// provider will use default value of 2
					bk.EXPECT().Resize(int64(2)).Return(nil).Times(1)

					scal.Do(ctx)
				})
			})
		})
	})

	g.Describe("Private Functions", func() {
		logger, _ := test.NewNullLogger()
		logger.SetOutput(g.GinkgoWriter)
		logger.SetLevel(logrus.DebugLevel)

		g.DescribeTable("isMinimumNodes", func(numNodes int, cronExpr string, result bool, disableWH ...bool) {
			opt := &Options{
				MinNodesInWorkingHours:      2,
				WorkingHoursCronExpressions: cronExpr,
			}

			if len(disableWH) > 0 {
				opt.DisableWorkingHours = true
			}

			scal := Scaler{
				logger:   logger.WithField("test", "private"),
				opt:      opt,
				schedule: gronx.New(),
			}

			o.Expect(scal.isMinimumNodes(MakeFakeNodes(numNodes))).To(o.Equal(result))
		},
			g.Entry("is 6 nodes isMinimumNodes in working hours", 6, "* * * * *", true),
			g.Entry("is 2 nodes isMinimumNodes in working hours", 2, "* * * * *", false),
			g.Entry("is 1 nodes isMinimumNodes in working hours", 1, "* * * * *", false),
			g.Entry("is 0 nodes isMinimumNodes in working hours", 0, "* * * * *", false),
			g.Entry("is 2 nodes isMinimumNodes in non working hours", 2, "@yearly", true),
			g.Entry("is 1 nodes isMinimumNodes in non working hours", 1, "@yearly", false),
			g.Entry("is 0 nodes isMinimumNodes in non working hours", 0, "@yearly", false),
			g.Entry("bad cron syntax", 0, "@jopka", false),
			g.Entry("disable working hourse check", 3, "@jopka", true, true),
		)
	})

})
