package scaler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
			cfg.Set("controller_node_name", "Built-In Node")
			cfg.Set("nodes_with_label", "JAS")
			cfg.Set("run_interval", "1s")
			cfg.Set("node_num_executors", "4")
			cfg.Set("gc_run_interval", "1s")
			cfg.Set("scale_up_threshold", "70")
			cfg.Set("scale_up_grace_period", "1s")
			cfg.Set("scale_down_grace_period", "10m")
			cfg.Set("err_grace_period", "200µs")
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
			g.It("should fail on error from jenkins api", func() {
				buf := new(bytes.Buffer)

				gwr := g.GinkgoWriter
				gwr.TeeTo(buf)

				scal.logger.Logger.SetOutput(gwr)

				msg := "api response status code 500"
				client.EXPECT().GetAllNodes(gomock.Any()).Return(nil, errors.New(msg)).Times(1)

				scal.GC(ctx)

				o.Expect(buf).Should(o.ContainSubstring(msg))
			})

			g.It("should start count on fail from jenkins master api", func() {
				respErr := true
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if respErr {
						w.WriteHeader(http.StatusBadRequest)
					} else {
						w.WriteHeader(http.StatusOK)

						json.NewEncoder(w).Encode(gojenkins.Computers{
							Computers: []*gojenkins.NodeResponse{
								{
									DisplayName: "0",
									AssignedLabels: []map[string]string{
										{
											"name": "JAS",
										},
									},
								},
								{
									DisplayName: "1",
									AssignedLabels: []map[string]string{
										{
											"name": "JAS",
										},
									},
								},
								{
									DisplayName: "3",
								},
								{
									DisplayName: "2",
									AssignedLabels: []map[string]string{
										{
											"name": "JAS",
										},
									},
								},
							},
						})
					}
				}))
				defer s.Close()

				opts := &jclient.Options{
					JenkinsURL:     s.URL,
					LastErrBackoff: 1 * time.Minute,
				}

				scal.client = jclient.New(opts)

				// skipping gc cause the error timer
				err := scal.gc(ctx, scal.logger)
				o.Expect(err).To(o.HaveOccurred())
				o.Expect(err.Error()).To(o.ContainSubstring("api response status code"))

				// skipping gc cause the error timer
				err = scal.gc(ctx, scal.logger)
				o.Expect(err).To(o.HaveOccurred())
				o.Expect(err.Error()).To(o.ContainSubstring("request rejected because jenkins API was in-accessible"))

				// retry gc again after err period passed
				opts.LastErrBackoff = 0
				respErr = false

				bk.EXPECT().Instances().Return(MakeFakeInstances(5), nil).Times(1)
				bk.EXPECT().Terminate(gomock.Any()).DoAndReturn(func(ins backend.Instances) error {
					o.Expect(ins).To(o.HaveLen(2))

					return nil
				}).Times(1)

				err = scal.gc(ctx, scal.logger)
				o.Expect(err).To(o.Not(o.HaveOccurred()))
			})

			g.It("clear 3 instances not registered in jenkins without label", func() {
				client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(4), nil).Times(1)
				// provider will decrease instances to 4
				bk.EXPECT().Instances().Return(MakeFakeInstances(7), nil).Times(1)
				bk.EXPECT().Terminate(gomock.Any()).DoAndReturn(func(ins backend.Instances) error {
					o.Expect(ins).To(o.HaveLen(3))

					return nil
				}).Times(1)

				client.EXPECT().DeleteNode(gomock.Any(), gomock.Any()).Return(true, nil).Times(3)

				cfg.Set("nodes_with_label", "")

				scal, err = New(cfg, bk, logger, metrics)
				o.Expect(err).To(o.Not(o.HaveOccurred()))

				scal.client = client

				scal.GC(ctx)
			})

			g.It("clear 2 instances not registered in jenkins with label", func() {
				client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(3, WithLabels([]string{"JAS"})), nil).Times(1)
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
				client.EXPECT().GetAllNodes(gomock.Any()).Return(MergeFakeTypes(
					MakeFakeNodes(5, WithLabels([]string{"JAS"})),
					MakeFakeNodes(2, WithLabels([]string{"JAS"}), WithOffline()),
				), nil).Times(1)
				bk.EXPECT().Instances().Return(MakeFakeInstances(5), nil).Times(1)
				client.EXPECT().DeleteNode(gomock.Any(), gomock.Any()).Return(true, nil).Times(2)

				scal.GC(ctx)
			})

			g.It("clear also if node is exist but in offline", func() {
				nodes := MergeFakeTypes(
					MakeFakeNodes(2, WithOffline(), WithLabels([]string{"JAS"})),
					MakeFakeNodes(4, WithLabels([]string{"JAS"})),
				)

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
			g.Context("set last error time from jenkins api", func() {
				g.It("set error from GetAllNodes", func() {
					buf := new(bytes.Buffer)

					gwr := g.GinkgoWriter
					gwr.TeeTo(buf)

					scal.logger.Logger.SetOutput(gwr)

					bk.EXPECT().Instances().Return(MakeFakeInstances(0), nil).Times(1)

					msg := "api response status code 500"
					client.EXPECT().GetAllNodes(gomock.Any()).Return(nil, errors.New(msg)).Times(1)

					scal.Do(ctx)

					o.Expect(buf).Should(o.ContainSubstring("can't get jenkins nodes: " + msg))
				})
			})

			g.Context("scaleUp", func() {
				g.It("run provider with 1 node, not in working hours, with usage over threshold", func() {
					cfg.Set("working_hours_cron_expressions", "@yearly")
					cfg.Set("nodes_with_label", "")

					scal, err = New(cfg, bk, logger, metrics)
					o.Expect(err).To(o.Not(o.HaveOccurred()))

					scal.client = client

					bk.EXPECT().Instances().Return(MakeFakeInstances(1), nil).Times(1)
					// 80% usage, and default scale up threshold is 70%
					// no running nodes
					client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(1, WithBusyExecutors(3)), nil).Times(1)

					// cloud backend have 1 running instances, this is default min value
					bk.EXPECT().CurrentSize().Return(int64(1), nil).Times(1)
					// backend will use new value of 2
					bk.EXPECT().Resize(int64(2)).Return(nil).Times(1)

					scal.Do(ctx)
				})

				g.It("run jenkins with min nodes and heavy usage, will scale out provider to 1 more", func() {
					bk.EXPECT().Instances().Return(MakeFakeInstances(1), nil).Times(1)
					// 75% usage, and default scale up threshold is 70%
					client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(1, WithBusyExecutors(3), WithLabels([]string{"JAS"})), nil).Times(1)
					// cloud backend have 1 running instances, this is default min value
					bk.EXPECT().CurrentSize().Return(int64(1), nil).Times(1)
					// provider will use new value of 2
					bk.EXPECT().Resize(int64(2)).Return(nil).Times(1)

					scal.Do(ctx)

					time.Sleep(time.Second)

					bk.EXPECT().Instances().Return(MakeFakeInstances(2), nil).Times(1)
					// 75% usage, and default scale up threshold is 70%
					client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(2, WithBusyExecutors(3), WithLabels([]string{"JAS"})), nil).Times(1)
					// cloud backend have 2 running instances, this is default min value
					bk.EXPECT().CurrentSize().Return(int64(2), nil).Times(1)
					// provider will use new value of 3
					bk.EXPECT().Resize(int64(3)).Return(nil).Times(1)

					scal.Do(ctx)

					time.Sleep(time.Second)

					bk.EXPECT().Instances().Return(MakeFakeInstances(5), nil).Times(1)
					// 100% usage, and default scale up threshold is 70%
					client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(5, WithBusyExecutors(4), WithLabels([]string{"JAS"})), nil).Times(1)
					// cloud backend have 5 running instances, this is default min value
					bk.EXPECT().CurrentSize().Return(int64(5), nil).Times(1)
					// provider will use new value of 8
					bk.EXPECT().Resize(int64(8)).Return(nil).Times(1)

					scal.Do(ctx)
				})
			})
			g.Context("scaleDown", func() {
				g.It("run provider with 5 nodes and low usage, will scale in provider, decrease from 5 to 4", func() {
					bk.EXPECT().Instances().Return(MakeFakeInstances(5), nil).Times(2)
					// 25% usage, and default scale down threshold is 30%
					// will return current provider running nodes
					nodes := MakeFakeNodes(5, WithBusyExecutors(1), WithLabels([]string{"JAS"}))
					client.EXPECT().GetAllNodes(gomock.Any()).Return(nodes, nil).Times(1)
					client.EXPECT().GetNode(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, name string) (*gojenkins.Node, error) {
						return nodes[name], nil
					}).Times(1)
					client.EXPECT().DeleteNode(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, name string) (bool, error) {
						delete(nodes, name)

						return true, nil
					}).Times(1)
					// provider will decrease instances to 4
					bk.EXPECT().Terminate(gomock.Any()).Return(nil).Times(1)

					scal.Do(ctx)

					// will show "still in grace period during working hours"

					// 25% usage, and default scale down threshold is 30%
					// will retrun current provider running nodes
					client.EXPECT().GetAllNodes(gomock.Any()).Return(nodes, nil).Times(1)

					scal.Do(ctx)
				})
			})
			g.Context("scaleToMinimum", func() {
				g.It("run provider with 1 node, in working hours, with usage under threshold", func() {
					bk.EXPECT().Instances().Return(MakeFakeInstances(1), nil).Times(1)
					// 45% usage and default scale up threshold is 70%
					// no running nodes
					client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(1), nil).Times(1)
					// provider will use default value of 1
					bk.EXPECT().Resize(int64(2)).Return(nil).Times(1)

					scal.Do(ctx)
				})
				g.It("run provider with 1 node, not in working hours, with usage under threshold", func() {
					cfg.Set("working_hours_cron_expressions", "@yearly")
					cfg.Set("nodes_with_label", "")

					scal, err = New(cfg, bk, logger, metrics)
					o.Expect(err).To(o.Not(o.HaveOccurred()))

					scal.client = client

					bk.EXPECT().Instances().Return(MakeFakeInstances(1), nil).Times(1)
					// 45% usage and default scale up threshold is 70%
					// no running nodes
					client.EXPECT().GetAllNodes(gomock.Any()).Return(MakeFakeNodes(1), nil).Times(1)

					scal.Do(ctx)
				})

				g.It("run provider with 0 nodes, not in working hours", func() {
					cfg.Set("working_hours_cron_expressions", "@yearly")

					scal, err = New(cfg, bk, logger, metrics)
					o.Expect(err).To(o.Not(o.HaveOccurred()))

					scal.client = client

					ins := MakeFakeInstances(0)
					bk.EXPECT().Instances().Return(ins, nil).Times(1)
					// no usage, cluster is empty of jobs, starting the day
					// no running nodes
					client.EXPECT().GetAllNodes(gomock.Any()).Return(make(jclient.Nodes), nil).Times(1)
					// provider will use default value of 1
					bk.EXPECT().Resize(int64(1)).Return(nil).Times(1)

					scal.Do(ctx)
				})

				g.It("run provider with 0 nodes, in working hours", func() {
					ins := MakeFakeInstances(0)
					bk.EXPECT().Instances().Return(ins, nil).Times(1)
					// no usage, cluster is empty of jobs, starting the day
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
				o.Expect(sclr.scaleDown(make(client.Nodes, 0), make(backend.Instances, 0))).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("no idle node was found"))
			})

			g.It("still in grace period during working hours log", func() {
				sclr.lastScaleDown = time.Now()

				o.Expect(sclr.scaleDown(make(client.Nodes, 0), make(backend.Instances, 0))).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("still in grace period during working hours. won't scale down"))
			})

			g.It("still in grace period outside working hours log", func() {
				sclr.lastScaleDown = time.Now()
				sclr.opt.WorkingHoursCronExpressions = "@yearly"

				o.Expect(sclr.scaleDown(make(client.Nodes, 0), make(backend.Instances, 0))).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("still in grace period outside working hours. won't scale down"))
			})

			g.It("removeNode: failed destroying, node is missing log", func() {
				client := mock_client.NewMockJenkinsAccessor(mockController)
				nodes := MakeFakeNodes(3)

				sclr.client = client

				client.EXPECT().GetNode(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, name string) (*gojenkins.Node, error) {
					return nil, errors.New("No node found")
				}).Times(3)

				o.Expect(sclr.scaleDown(nodes, MakeFakeInstances(3))).To(o.Not(o.HaveOccurred()))
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

				o.Expect(sclr.scaleDown(nodes, MakeFakeInstances(3))).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("failed destroying .* with error can't delete node from jenkins. continue to next node"))
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

				o.Expect(sclr.scaleDown(nodes, MakeFakeInstances(0))).To(o.Not(o.HaveOccurred()))
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

				o.Expect(sclr.scaleDown(nodes, MakeFakeInstances(3))).To(o.Not(o.HaveOccurred()))
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

				bk.EXPECT().Terminate(gomock.Any()).DoAndReturn(func(ins backend.Instances) error {
					o.Expect(ins).To(o.HaveLen(1))

					return nil
				}).Times(1)

				o.Expect(sclr.scaleDown(nodes, MakeFakeInstances(3))).To(o.Not(o.HaveOccurred()))
				o.Expect(buffer).To(gbytes.Say("instance .* was removed from the backen"))
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
