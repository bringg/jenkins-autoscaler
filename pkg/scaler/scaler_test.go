package scaler

import (
	"context"
	"errors"
	"time"

	"github.com/adhocore/gronx"
	"github.com/bndr/gojenkins"
	"github.com/golang/mock/gomock"
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"

	"github.com/bringg/jenkins-autoscaler/pkg/backend"
	"github.com/bringg/jenkins-autoscaler/pkg/scaler/client"
	jclient "github.com/bringg/jenkins-autoscaler/pkg/scaler/client"
	mock_backend "github.com/bringg/jenkins-autoscaler/pkg/testing/mocks/backend"
	mock_client "github.com/bringg/jenkins-autoscaler/pkg/testing/mocks/scaler"
)

var _ = g.Describe("Scaler", func() {
	g.Describe("Public Functions", func() {
		var scal *Scaler
		var ctx context.Context
		var err error
		var client *mock_client.MockJenkinsAccessor
		var bk *mock_backend.MockBackend
		var logger *logrus.Logger
		var cfg configmap.Simple

		var mockController *gomock.Controller

		metrics := NewMetrics()

		g.BeforeEach(func() {
			mockController, ctx = gomock.WithContext(context.Background(), g.GinkgoT())

			client = mock_client.NewMockJenkinsAccessor(mockController)
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

			scal, err = New(cfg, bk, logger, metrics)
			o.Expect(err).To(o.Not(o.HaveOccurred()))
			scal.client = client
		})

		g.AfterEach(func() {
			mockController.Finish()
		})

		g.Describe("GC", func() {
			g.It("clear 2 instances not registered in jenkins", func() {
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

			g.It("should clear zombies from jenkins when instance is missing in backend", func() {
				client.EXPECT().GetAllNodes(gomock.Any()).Return(MergeFakeTypes(MakeFakeNodes(5), MakeFakeNodes(2, WithOffline())), nil).Times(1)
				bk.EXPECT().Instances().Return(MakeFakeInstances(5), nil).Times(1)
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

					scal, err = New(cfg, bk, logger, metrics)
					o.Expect(err).To(o.Not(o.HaveOccurred()))

					scal.client = client

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

				g.It("run provider with min nodes and heavy usage, will scale out provider to 1 more", func() {
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

					scal, err = New(cfg, bk, logger, metrics)
					o.Expect(err).To(o.Not(o.HaveOccurred()))

					scal.client = client

					// 45% usage and default scale up threshold is 70%
					client.EXPECT().GetCurrentUsage(gomock.Any()).Return(int64(45), nil).Times(1)
					// no running nodes
					client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(1), nil).Times(1)

					scal.Do(ctx)
				})

				g.It("run provider with 0 nodes, not in working hours", func() {
					cfg.Set("working_hours_cron_expressions", "@yearly")

					scal, err = New(cfg, bk, logger, metrics)
					o.Expect(err).To(o.Not(o.HaveOccurred()))

					scal.client = client

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
		g.Describe("scaleDown", func() {
			var sclr *Scaler
			var logger *logrus.Logger
			var buffer *gbytes.Buffer
			var mockController *gomock.Controller

			g.BeforeEach(func() {
				mockController = gomock.NewController(g.GinkgoT())
				logger, _ = test.NewNullLogger()
				buffer = gbytes.NewBuffer()

				gw := g.GinkgoWriter
				gw.TeeTo(buffer)

				logger.SetOutput(gw)
				logger.SetLevel(logrus.DebugLevel)

				sclr = &Scaler{
					opt: &Options{
						ScaleDownGracePeriod:                   fs.Duration(time.Minute * 1),
						ScaleDownGracePeriodDuringWorkingHours: fs.Duration(time.Minute * 1),
						WorkingHoursCronExpressions:            "* * * * *",
					},
					schedule: gronx.New(),
					logger:   logrus.NewEntry(logger),
				}
			})

			g.It("no idle node was found log", func() {
				o.Expect(sclr.scaleDown(make(client.Nodes, 0))).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("no idle node was found"))
			})

			g.It("still in grace period during working hours log", func() {
				sclr.lastScaleDown = time.Now()

				o.Expect(sclr.scaleDown(make(client.Nodes, 0))).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("still in grace period during working hours. won't scale down"))
			})

			g.It("still in grace period outside working hours log", func() {
				sclr.lastScaleDown = time.Now()
				sclr.opt.WorkingHoursCronExpressions = "@yearly"

				o.Expect(sclr.scaleDown(make(client.Nodes, 0))).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("still in grace period outside working hours. won't scale down"))
			})

			g.It("removeNode: failed destroying, node is missing log", func() {
				client := mock_client.NewMockJenkinsAccessor(mockController)
				nodes := MakeFakeNodes(3)

				sclr.client = client

				client.EXPECT().GetNode(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, name string) (*gojenkins.Node, error) {
					return nil, errors.New("No node found")
				}).Times(3)

				o.Expect(sclr.scaleDown(nodes)).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("failed destroying .* with error No node found. continue to next node"))
				o.Expect(buffer).To(gbytes.Say("no idle node was found"))
			})

			g.It("removeNode: failed destroying, can't delete node from jenkins cluster log", func() {
				client := mock_client.NewMockJenkinsAccessor(mockController)
				nodes := MakeFakeNodes(3)

				sclr.client = client

				client.EXPECT().GetNode(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, name string) (*gojenkins.Node, error) {
					return nodes[name], nil
				}).Times(3)

				client.EXPECT().DeleteNode(gomock.Any(), gomock.Any()).Return(false, nil).Times(3)

				o.Expect(sclr.scaleDown(nodes)).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("failed destroying .* with error can't delete node from jenkins cluster. continue to next node"))
				o.Expect(buffer).To(gbytes.Say("no idle node was found"))
			})

			g.It("removeNode: failed destroying, can't terminate instance is missing log", func() {
				client := mock_client.NewMockJenkinsAccessor(mockController)
				bk := mock_backend.NewMockBackend(mockController)
				nodes := MakeFakeNodes(3)

				sclr.client = client
				sclr.backend = bk

				client.EXPECT().GetNode(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, name string) (*gojenkins.Node, error) {
					return nodes[name], nil
				}).Times(3)

				client.EXPECT().DeleteNode(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)

				bk.EXPECT().Instances().Return(MakeFakeInstances(0), nil).Times(3)

				o.Expect(sclr.scaleDown(nodes)).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("failed destroying .* with error can't terminate instance .* is missing. continue to next node"))
				o.Expect(buffer).To(gbytes.Say("no idle node was found"))
			})

			g.It("removeNode: can't remove current node, node is in use log", func() {
				client := mock_client.NewMockJenkinsAccessor(mockController)
				nodes := MakeFakeNodes(3, WithoutIdle())

				sclr.client = client

				client.EXPECT().GetNode(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, name string) (*gojenkins.Node, error) {
					return nodes[name], nil
				}).Times(3)

				o.Expect(sclr.scaleDown(nodes)).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("node name .*: can't remove current node, node is in use"))
				o.Expect(buffer).To(gbytes.Say("no idle node was found"))
			})

			g.It("removeNode: successfully remove first node log", func() {
				client := mock_client.NewMockJenkinsAccessor(mockController)
				bk := mock_backend.NewMockBackend(mockController)
				nodes := MakeFakeNodes(3)

				sclr.client = client
				sclr.backend = bk

				client.EXPECT().GetNode(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, name string) (*gojenkins.Node, error) {
					return nodes[name], nil
				}).Times(1)

				client.EXPECT().DeleteNode(gomock.Any(), gomock.Any()).Return(true, nil).Times(1)

				bk.EXPECT().Instances().Return(MakeFakeInstances(3), nil).Times(1)

				bk.EXPECT().Terminate(gomock.Any()).DoAndReturn(func(ins backend.Instances) error {
					o.Expect(ins).To(o.HaveLen(1))

					return nil
				}).Times(1)

				o.Expect(sclr.scaleDown(nodes)).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("node .* was removed from cluster"))
				o.Expect(buffer).ShouldNot((gbytes.Say("no idle node was found")))
			})
		})

		g.Describe("scaleUp", func() {
			var sclr *Scaler
			var logger *logrus.Logger
			var buffer *gbytes.Buffer
			var mockController *gomock.Controller

			g.BeforeEach(func() {
				mockController = gomock.NewController(g.GinkgoT())
				logger, _ = test.NewNullLogger()
				buffer = gbytes.NewBuffer()

				gw := g.GinkgoWriter
				gw.TeeTo(buffer)

				logger.SetOutput(gw)
				logger.SetLevel(logrus.DebugLevel)

				sclr = &Scaler{
					opt: &Options{
						ScaleUpGracePeriod: fs.Duration(time.Minute * 1),
					},
					logger: logrus.NewEntry(logger),
				}
			})

			g.It("grace period log", func() {
				// set scaleeUp to get log message
				sclr.lastScaleUp = time.Now()

				o.Expect(sclr.scaleUp(0)).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("still in grace period. won't scale up"))
			})

			g.It("reached maximum nodes log", func() {
				bk := mock_backend.NewMockBackend(mockController)

				sclr.backend = bk
				sclr.opt.MaxNodes = 3

				bk.EXPECT().CurrentSize().Return(int64(3), nil).Times(1)

				o.Expect(sclr.scaleUp(0)).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("reached maximum of %d nodes. won't scale up", sclr.opt.MaxNodes))
			})

			g.It("skipping resize cause new size is the same as current log", func() {
				bk := mock_backend.NewMockBackend(mockController)

				sclr.backend = bk
				sclr.opt.MaxNodes = 12
				sclr.opt.ScaleUpThreshold = 70

				bk.EXPECT().CurrentSize().Return(int64(9), nil).Times(1)

				o.Expect(sclr.scaleUp(70)).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("new target size: 9 = 9 is the same as current, skipping the resize"))
			})

			g.It("more then maximum nodes log", func() {
				bk := mock_backend.NewMockBackend(mockController)

				sclr.backend = bk
				sclr.opt.MaxNodes = 12
				sclr.opt.ScaleUpThreshold = 70

				bk.EXPECT().CurrentSize().Return(int64(9), nil).Times(1)
				bk.EXPECT().Resize(sclr.opt.MaxNodes).Return(nil).Times(1)

				o.Expect(sclr.scaleUp(100)).To(o.Not(o.HaveOccurred()))
				// will set the number to maximum size of 12
				o.Expect(buffer).To(gbytes.Say("need 4 extra nodes, but can't go over the limit of %d", sclr.opt.MaxNodes))
				o.Expect(buffer).To(gbytes.Say("will spin up 3 extra nodes"))
				o.Expect(buffer).To(gbytes.Say("new target size: %d", sclr.opt.MaxNodes))
			})

			g.It("dry run mode log", func() {
				bk := mock_backend.NewMockBackend(mockController)

				sclr.backend = bk
				sclr.opt.MaxNodes = 12
				sclr.opt.ScaleUpThreshold = 70
				sclr.opt.DryRun = true

				bk.EXPECT().CurrentSize().Return(int64(9), nil).Times(1)

				o.Expect(sclr.scaleUp(100)).To(o.Not(o.HaveOccurred()))
				// will set the number to maximum size of 12
				o.Expect(buffer).To(gbytes.Say("need 4 extra nodes, but can't go over the limit of %d", sclr.opt.MaxNodes))
				o.Expect(buffer).To(gbytes.Say("will spin up 3 extra nodes"))
				o.Expect(buffer).To(gbytes.Say("new target size: %d", sclr.opt.MaxNodes))
			})
		})

		g.DescribeTable("isMinimumNodes", func(numNodes int, cronExpr string, result bool, disableWH ...bool) {
			logger, _ := test.NewNullLogger()
			logger.SetOutput(g.GinkgoWriter)
			logger.SetLevel(logrus.DebugLevel)

			opt := &Options{
				MinNodesInWorkingHours:      2,
				WorkingHoursCronExpressions: cronExpr,
			}

			if len(disableWH) > 0 {
				opt.DisableWorkingHours = true
			}

			scal := Scaler{
				logger:   logrus.NewEntry(logger),
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
			g.Entry("disable working hours check", 3, "@jopka", true, true),
		)
	})

})
